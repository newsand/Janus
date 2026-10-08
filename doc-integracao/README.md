# Integrando um produto com o Janus

Guia completo de como qualquer produto (front + back próprios) integra com o
**Janus** (`../janus`) — o serviço de identidade JWT deste workspace. Documenta
exatamente o padrão usado na integração de referência **crazy-back + crazy-front**
(`../crazy-back`, `../crazy-front`), com endpoints, payloads reais e trechos de código.

> **Versão interativa:** abra [`index.html`](index.html) no navegador (duplo clique, sem servidor). Mesmo conteúdo deste arquivo, com diagrama de cada fluxo e dois modos de leitura (Dev e IA). O sistema visual está em [`MASTER.md`](MASTER.md). Ao mudar este README, atualize o `index.html` junto.

> **Regra de ouro do Janus:** ele só prova identidade (`sub` = UUID do usuário) e
> emite JWT. **Não tem roles, não tem RBAC, não redireciona nada.** Papéis, permissões e
> qualquer dado de produto vivem **no seu backend**, nunca aqui. Ver
> `../janus/AUTH-MVP.md` §1 para o contrato de produto completo.

## Sumário

1. [Arquitetura da integração](#1-arquitetura-da-integração)
2. [Configuração e segredos](#2-configuração-e-segredos)
3. [Autenticação (login, refresh, logout, /me)](#3-autenticação-login-refresh-logout-me)
4. [Validar o JWT no seu backend (sem round-trip)](#4-validar-o-jwt-no-seu-backend-sem-round-trip)
5. [Roles: como o produto mapeia identidade → papel](#5-roles-como-o-produto-mapeia-identidade--papel)
6. [CRUD de usuários (service key)](#6-crud-de-usuários-service-key)
7. [Convite por magic link](#7-convite-por-magic-link)
8. [Magic link de login (usuário já existente)](#8-magic-link-de-login-usuário-já-existente)
9. [Recuperação de senha](#9-recuperação-de-senha)
10. [2FA (opcional)](#10-2fa-opcional)
11. [Erros, rate limit e lockout](#11-erros-rate-limit-e-lockout)
12. [Checklist de integração](#12-checklist-de-integração)

---

## 1. Arquitetura da integração

```
┌───────────────┐       ┌──────────────────────┐       ┌────────────────┐
│ seu frontend  │──────▶│ seu backend          │──────▶│ Janus          │
│ (browser)     │  API  │ (guarda roles e      │ HTTP  │ (identidade    │
│               │◀──────│ dados de produto)    │◀──────│ + JWT)         │
└───────────────┘       └──────────────────────┘       └────────────────┘
```

**Regra central: o frontend nunca fala com o Janus diretamente.** Ele fala só com o
seu próprio backend, que é quem:

- repassa login/refresh/logout para o Janus;
- guarda a **service key** do Janus (ela nunca chega ao browser);
- guarda **roles e qualquer dado de produto** numa tabela local, indexada pelo `sub`
  (UUID) do usuário;
- valida o JWT localmente (sem chamar o Janus a cada request — ver §4).

É exatamente assim que o `crazy-back` está montado:

| Peça | Onde | Papel |
|---|---|---|
| `crazy-front` | `../crazy-front` | só chama `/api/*` do `crazy-back` |
| `crazy-back` | `../crazy-back` | chama o Janus, guarda `Profile{role}` local |
| `janus` | `../janus` | identidade + JWT, nenhuma role |

---

## 2. Configuração e segredos

Seu backend precisa de três variáveis (ver `crazy-back/internal/config/config.go`):

| Variável | Exemplo | Uso |
|---|---|---|
| `JWT_SECRET` | mesmo valor do Janus | validar o JWT localmente (HS256) |
| `JANUS_URL` | `http://localhost:8080` | base URL para as chamadas HTTP |
| `JANUS_SERVICE_KEY` | uma das `SERVICE_KEYS` do Janus | CRUD de usuários e invites |

O Janus precisa ter, no seu próprio `.env`/ambiente:

```env
JWT_SECRET=<mesmo segredo usado acima — 45+ chars>
SERVICE_KEYS=chave-do-seu-produto-45+chars,chave-anterior-para-rotacao-45+chars
```

**Mínimo de 45 caracteres** para o `JWT_SECRET` e **para cada** chave de `SERVICE_KEYS`: sem
`DEV_ENV=true`, o Janus recusa iniciar com segredo menor (e com `SERVICE_KEYS`
vazio). Gere com `openssl rand -base64 48`. Como o `JWT_SECRET` do seu backend precisa ser
idêntico ao do Janus, use o mesmo valor longo nos dois. `DEV_ENV=true` desliga essa
validação e serve **só para teste local** (o `docker-compose.yml` e o `.env.example` deste
workspace já o ligam); nunca em produção.

O `crazy-back` aplica a mesma regra ao próprio ambiente
(`crazy-back/internal/config/config.go:Validate`, chamado no `main.go`): sem
`DEV_ENV=true`, ele não inicia se `JWT_SECRET` ou `JANUS_SERVICE_KEY` tiverem menos de
45 caracteres. Faça o mesmo no seu backend.

`SERVICE_KEYS` é uma lista (rotação sem downtime): mantenha pelo menos duas ativas.
Nunca gere uma service key própria fora dessa env — o Janus valida por
comparação de tempo constante contra essa lista (`internal/middleware/auth.go` do
Janus, `ServiceKeyAuth()`).

---

## 3. Autenticação (login, refresh, logout, /me)

Seu backend expõe endpoints próprios que **fazem proxy** para o janus. Referência:
`crazy-back/features/auth/handler.go`.

### Login

```
POST /api/auth/login          (seu backend)
  → POST /v1/auth/login       (janus, sem auth)
```

Payload de entrada (igual ao contrato do Janus):
```json
{ "email": "user@example.com", "password": "senha123" }
```

O Janus responde:
```json
{
  "access_token": "eyJ...",
  "refresh_token": "a1b2c3...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

Seu backend deve:
1. repassar `email`/`password` pro Janus (`POST /v1/auth/login`);
2. extrair o `sub` do `access_token` retornado (mesmo segredo, dá pra decodificar sem
   round-trip — ver `crazy-back/features/auth/handler.go:subjectOf`);
3. buscar o registro local (`Profile`) por esse `sub` pra pegar a `role`;
4. devolver pro seu frontend os tokens **+ os dados de produto** (role, etc.):

```json
{
  "access_token": "eyJ...",
  "refresh_token": "a1b2c3...",
  "token_type": "Bearer",
  "expires_in": 900,
  "user": { "id": "<uuid>", "email": "...", "nome": "...", "role": "admin" }
}
```

Se `2fa_enabled` estiver ligado pro usuário, o Janus responde diferente (sem
tokens ainda) — ver [§10](#10-2fa-opcional).

**Falha de autenticação é uma só.** Usuário inexistente, usuário sem senha (convite não
aceito), senha errada e **conta desabilitada** respondem todos `401 {"error":"invalid
credentials"}` — mesmo status, mesmo código, mesmo corpo, mesmo custo de tempo (sempre um
bcrypt). **Não existe `account disabled` na resposta de login.** Consequências pro seu
produto:

- Seu backend **não pode** criar uma resposta diferente para "conta desativada". Se o
  `Profile` local estiver `Active=false` depois de uma senha certa, devolva o mesmo
  `401 {"error":"invalid credentials"}` (é o que `crazy-back/features/auth/handler.go`
  faz) — nunca `account disabled`, `403` ou outro texto. O mesmo vale para "senha certa mas
  sem `Profile` local": também `401 invalid credentials`, nunca `403`, para não confirmar
  que a senha estava certa.
- Não mostre no front mensagens que separem os casos; "E-mail ou senha incorretos" serve
  para todos. Avisos de desativação vão por e-mail/suporte, não pelo login.
- Senha errada conta no lockout (inclusive para conta desabilitada, e para e-mail
  inexistente — o `429 account temporarily locked` não revela se o e-mail existe). Senha
  certa em conta desabilitada não emite token e não mexe no lockout.
- O refresh de usuário desabilitado responde `401 invalid refresh token` (antes:
  `account disabled`).

Contrato original: `janus/AUTH-MVP.md` §7, "Login — falha de autenticação é uma só".

### Refresh

```
POST /api/auth/refresh  →  POST /v1/auth/refresh
```
```json
{ "refresh_token": "a1b2c3..." }
```
Resposta: novo par `access_token`/`refresh_token` (rotação automática — o antigo é
invalidado; reapresentá-lo depois disso é tratado como reuse e revoga a família inteira
de refresh tokens daquele usuário). Repasse 1:1, sem lógica extra.

### Logout

```
POST /api/auth/logout  (Bearer access_token)  →  POST /v1/auth/logout
```
Revoga os refresh tokens do usuário no janus. Repasse o header `Authorization`
recebido do seu próprio front direto pro janus.

### /me (identidade pura, sem role)

```
GET /v1/me   (Bearer access_token)
```
```json
{ "id": "<uuid>", "email": "...", "nome": "...", "2fa_enabled": false }
```

Isso é **só identidade**. Se seu produto quer expor `/api/auth/me` pro frontend com a
`role` junto, monte a resposta você mesmo a partir do `Profile` local (não precisa nem
chamar `/v1/me` de novo — o middleware do seu backend já validou o JWT e sabe o `sub`).

---

## 4. Validar o JWT no seu backend (sem round-trip)

**Não chame o Janus a cada request autenticado.** O JWT é HS256 assinado com
`JWT_SECRET` — se seu backend tem o mesmo segredo, valida localmente. Isso é o que faz
`crazy-back/internal/middleware/auth.go`:

```go
token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
    if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
        return nil, jwt.ErrSignatureInvalid
    }
    return []byte(cfg.JWTSecret), nil
})
// claims.Subject == o UUID do usuário (o "sub" do JWT)
```

Claims do access token (fechado, não muda — ver `AUTH-MVP.md` §3):

| Claim | Sempre presente | Notas |
|---|---|---|
| `sub` | sim | UUID do usuário — é a sua chave pra tudo |
| `iat`/`exp` | sim | access token dura 15 min (`ACCESS_TOKEN_TTL`) |
| `jti` | sim | id do access, pra revoke pontual (raramente usado) |
| `2fa_verified` | sim | `true` se 2FA off ou já verificado |

**Nunca existe `roles` ou `tenant` no token.** Depois de validar a assinatura, busque a
role no seu banco pelo `sub` — nunca confie em nada além do `sub` vindo do token.

Depois de decodificar, seu middleware busca o registro local e injeta no contexto do
request (`crazy-back/internal/middleware/auth.go:JWTAuth`):

```go
var profile models.Profile
db.DB().First(&profile, "id = ?", claims.Subject)
if !profile.Active { /* 401 {"error":"invalid token"} — usuário desativado no SEU produto */ }
c.Set("user_id", profile.ID)
c.Set("role", profile.Role)
```

(Use uma mensagem genérica — `invalid token` — e não `account disabled`: ver
[§3, "Falha de autenticação é uma só"](#login).)

Um middleware `RequireAdmin()` (ou equivalente) simplesmente checa
`c.Get("role") == "admin"` — de novo, tudo local, zero chamada ao janus.

---

## 5. Roles: como o produto mapeia identidade → papel

Modelo de referência (`crazy-back/internal/models/models.go`):

```go
type Profile struct {
    ID     string // == sub do JWT == users.id do janus
    Email  string
    Nome   string
    Role   string // "admin" | "user" — só existe no SEU banco
    Active bool   // soft-disable local, independente do disabled_at do janus
}
```

Regras:
- **O `id` do `Profile` é sempre o mesmo UUID que o Janus usa.** Nunca gere um id
  próprio — isso quebraria a correlação com o JWT.
- Toda vez que você cria um usuário (via CRUD ou invite), crie o `Profile` local **na
  mesma operação**, com a role escolhida.
- Toda vez que você desativa um usuário no seu produto, desative também no Janus
  (`PATCH /v1/users/:id {"disabled_at": "..."}`) — senão o usuário continua conseguindo
  logar (identidade) mesmo estando "removido" no seu produto.

---

## 6. CRUD de usuários (service key)

Só o **seu backend** chama isso — nunca o frontend, nunca com um JWT de usuário.
Header sempre `Authorization: Bearer <service_key>`.

### Criar usuário

```
POST /v1/users
Authorization: Bearer <service_key>
```
```json
{ "email": "user@example.com", "nome": "Nome", "password": "senha123", "telefone": "opcional", "cpf": "opcional" }
```
`nome` é obrigatório e **aparado** (`TrimSpace`): vazio ou só espaços → `400`, e o valor
gravado não tem espaços nas pontas. A mesma regra vale no `PATCH /v1/users/:id` (abaixo) e
no endpoint de criação do seu próprio backend — valide/apare lá também, antes de chamar o
Janus (`crazy-back/features/admin/handler.go:CreateUser` faz isso).

`password` é opcional aqui — sem ela o usuário fica sem `password_hash` (útil pro fluxo
de invite: cria-se o usuário sem senha, e só o accept do invite define a senha).
Resposta `201`:
```json
{ "id": "<uuid>", "email": "...", "nome": "...", "created_at": "..." }
```
Depois disso, seu backend cria o `Profile` local com a role e qualquer dado inicial de
produto (`crazy-back/features/admin/handler.go:CreateUser`).

### Listar / buscar por e-mail

```
GET /v1/users?page=1     # lista paginada (100 por página; usada pra idempotência)
GET /v1/users/:id
```

A listagem é **paginada, 100 usuários por página**, ordenada do mais recente para o mais
antigo. `page` começa em 1 (padrão); `page` inválido (`0`, negativo, não numérico) → `400`.
Uma página além da última volta vazia (`users: []`).

```json
{
  "users": [ { "id": "...", "email": "...", "nome": "..." } ],
  "page": 1,
  "page_size": 100,
  "total": 250,
  "total_pages": 3
}
```

O Janus **não tem busca por e-mail**: para achar um usuário pelo e-mail, percorra as
páginas até `page >= total_pages` comparando o campo `email` (veja
`crazy-back/internal/janus/client.go:FindUserByEmail`). Quem lê só a primeira página
enxerga apenas as 100 contas mais recentes.

### Atualizar / desativar (soft-delete)

```
PATCH /v1/users/:id
Authorization: Bearer <service_key>
```
```json
{ "disabled_at": "2026-09-17T00:00:00Z" }
```
Desativa a identidade — **não existe DELETE**, é sempre soft-disable. Para reativar,
mande `{"disabled_at": ""}`. O mesmo endpoint também aceita atualizar `email`, `nome`,
`telefone`, `cpf` (todos opcionais, manda só o que for mudar). Se mandar `nome`, ele é
aparado e não pode ficar vazio (`400`).

### Trocar e-mail de outro usuário

```json
{ "email": "novo@example.com" }
```
- E-mail já usado por outra conta → `409`.
- **Trocar o e-mail revoga todos os refresh tokens do usuário** (mesma revogação do
  bloqueio). O usuário precisa logar de novo com o e-mail novo. Os demais campos
  (`nome`, `telefone`, `cpf`) **não** revogam nada.

### Bloqueio por segurança (encerrar as sessões de um usuário)

Não existe endpoint "só revogar sessões": o bloqueio **é** o mecanismo. Use quando
suspeitar de conta comprometida, fraude ou for preciso derrubar alguém já.

```
PATCH /v1/users/:id
Authorization: Bearer <service_key>
{ "disabled_at": "2026-09-17T00:00:00Z" }
```
(qualquer valor não vazio bloqueia; o Janus grava o horário atual do servidor)

O que acontece **na hora**:
1. Todos os refresh tokens ainda ativos do usuário (todos os dispositivos) são revogados.
2. `login`, `refresh` e `magic-link/consume` passam a ser recusados para essa conta
   (login com senha correta também não emite token).

O que **não** acontece (limitação conhecida — leia):
- **Access tokens já emitidos continuam válidos até expirar** (`ACCESS_TOKEN_TTL`,
  padrão 15 min). O middleware do Janus valida só assinatura/expiração e **não**
  consulta `disabled_at` a cada request. A revogação impede apenas a *renovação* da sessão.
- Para corte **imediato** no seu produto, faça os dois: bloqueie no Janus **e** marque
  o `Profile` local como inativo (`Active=false`), checando isso no seu próprio
  middleware a cada request — como o `crazy-back` faz.

Reverter: `{"disabled_at": ""}` reativa a conta, mas os refresh tokens revogados **não**
voltam; o usuário faz login novamente.

Resumo:

| Ação (`PATCH /v1/users/:id`) | Revoga refresh tokens | Bloqueia login | Access token vigente |
|---|---|---|---|
| `disabled_at` preenchido | sim | sim | vale até expirar |
| `email` alterado | sim | não | vale até expirar |
| `nome`/`telefone`/`cpf` | não | não | — |
| `disabled_at: ""` | não | libera | — |

---

## 7. Convite por magic link

Fluxo completo pra convidar alguém que ainda não tem conta (implementado em
`crazy-back/features/admin/handler.go:CreateInvite` +
`crazy-back/features/invite/handler.go`).

### 7.1 — Seu backend cria o convite

```
POST /v1/invites
Authorization: Bearer <service_key>
```
```json
{ "email": "novo@example.com" }
```

Resposta (com `MAILER_STUB=true`, o padrão em dev):
```json
{
  "message": "invite sent",
  "email": "novo@example.com",
  "token": "a1b2c3...",
  "expires_at": "2026-09-24T16:17:21Z"
}
```

`token`/`expires_at` só existem com `MAILER_STUB=true`. Com um mailer real
(`MAILER_STUB=false`), o Janus entrega o convite por e-mail e **não** devolve o
token — nesse caso seu backend não tem como montar o link sozinho; o e-mail enviado
pelo Janus é quem carrega o link (ajuste o template de e-mail para apontar pro
`accept_url` do seu produto, não pro Janus).

Seu backend deve:
1. guardar localmente `{ email, role escolhida, hash do token, expires_at }` — o
   Janus só guarda o hash, guardar de novo localmente é o que permite ao seu
   backend saber **qual role** aplicar quando o convite for aceito (o Janus não
   tem esse conceito). Ver `crazy-back/internal/models/models.go:Invite` +
   `HashInviteToken` (SHA-256 simples, calculado no seu lado).
2. montar o link: `<seu-frontend>/accept-invite?token=<token>`.
3. mostrar/copiar esse link pro admin enviar (ou, com mailer real, nem chega a isso).

### 7.2 — O convidado aceita (endpoint público, sem auth)

Seu backend expõe um endpoint público (sem JWT, sem service key) que:

```
POST /api/invite/accept   (seu backend, público)
```
```json
{ "token": "a1b2c3...", "password": "novaSenha123", "nome": "Nome Completo" }
```

Internamente ele:
1. recalcula o hash do `token` recebido e busca o convite local por esse hash —
   confere se já foi usado / expirou **antes** de gastar uma chamada no Janus;
2. chama `POST /v1/invites/accept` no Janus:
   ```
   POST /v1/invites/accept
   ```
   ```json
   { "token": "a1b2c3...", "password": "novaSenha123", "nome": "Nome Completo" }
   ```
   Resposta: `{ "message": "account activated", "user_id": "<uuid>" }`. Esse endpoint é
   **público no Janus também** (rate-limited) — o token opaco É a autenticação.
3. cria o `Profile` local (`id = user_id`, `role` = a que foi escolhida no passo 7.1),
   gera qualquer dado inicial do seu produto;
4. marca o convite local como aceito;
5. (opcional, mas é o que o `crazy-back` faz) já loga o usuário: chama
   `POST /v1/auth/login` com o e-mail + a senha que ele acabou de definir, e devolve os
   tokens pro frontend — assim o convidado já cai logado, sem precisar digitar a senha
   de novo.

Regras do Janus que valem a pena saber (`AUTH-MVP.md` §7 "Invite"):
- Token amarra o e-mail: aceitar com outro e-mail é rejeitado.
- Um novo invite pro mesmo e-mail invalida os invites anteriores desse e-mail.
- Convite não é signup aberto: só quem tem o token entra.

---

## 8. Magic link de login (usuário já existente)

Diferente do invite (que é pra criar conta), isto é "esqueci minha senha mas quero só
entrar" / passwordless login pra quem **já tem conta ativa**.

```
POST /v1/auth/magic-link
```
```json
{ "email": "user@example.com" }
```
Resposta **sempre genérica** (não revela se o e-mail existe):
```json
{ "message": "if the email exists, a magic link will be sent" }
```

Com `MAILER_STUB=true` o token vai só pro **log do container** do Janus (não tem
retorno de token na API pra esse endpoint — diferente do invite, aqui não há service key
na chamada, então liberar o token na resposta seria um risco real de enumeração). Para
usar isso de verdade em dev, leia o log do Janus (`docker compose logs -f app`) ou
implemente um mailer real antes de habilitar esse fluxo pro usuário final.

```
POST /v1/auth/magic-link/consume
```
```json
{ "token": "a1b2c3..." }
```
Resposta: mesmo shape do login (`access_token`/`refresh_token`/...), ou o desafio de 2FA
se `2fa_enabled`. Bloqueado (401) se o usuário ainda não tiver senha definida (convite
não aceito) — evita conta zumbi.

---

## 9. Recuperação de senha

```
POST /v1/password/forgot
```
```json
{ "email": "user@example.com" }
```
Resposta sempre genérica (mesma lógica anti-enumeração do magic-link). Token só no log
(`MAILER_STUB=true`) — não volta na API.

```
POST /v1/password/reset
```
```json
{ "token": "a1b2c3...", "new_password": "novaSenha123" }
```
Troca a senha, consome o token, **revoga todas as famílias de refresh token** do
usuário (força novo login em todo lugar). Seu backend não precisa fazer nada além de
repassar essas duas chamadas — não há role/Profile envolvido aqui.

---

## 10. 2FA (opcional)

Só implemente se seu produto realmente For usar. Fluxo (ver
`janus/features/twofa/handler.go`):

1. **Ativar:** `POST /v1/me/2fa/enable` (Bearer access) sem body → devolve `secret`
   (mostrar como QR code). Chamar de novo **com** `{"code": "123456"}` (código do app
   autenticador) pra confirmar e ligar de fato.
2. **Login com 2FA ligado:** `POST /v1/auth/login` não devolve tokens — devolve
   `{"requires_2fa": true, "challenge_token": "..."}`. Seu frontend pede o código e
   chama:
   ```
   POST /v1/auth/2fa/verify
   { "challenge_token": "...", "code": "123456" }
   ```
   que aí sim devolve os tokens normais.
3. **Desativar:** `POST /v1/me/2fa/disable` com `{"password": "..."}` **ou**
   `{"code": "..."}` (reauth — sessão roubada sozinha não desliga 2FA).

Nada disso toca em `Profile`/role — é inteiramente gerenciado pelo janus.

---

## 11. Erros, rate limit e lockout

- Erros do Janus vêm como `{"error": "mensagem"}` com o status HTTP apropriado
  (400/401/403/404/409/429/500). Repasse o `error` pro seu frontend quando fizer proxy
  (ver `crazy-back/features/auth/handler.go` — todo handler de proxy relay o body de
  erro do upstream). O proxy repassa **status e corpo** como vieram: por exemplo, o
  `429 account temporarily locked` do lockout chega ao front como 429, não como 401
  (`janus.Failure` em `crazy-back/internal/janus/client.go`).
- **Falha de login é sempre `401 {"error":"invalid credentials"}`**, seja e-mail
  inexistente, sem senha, senha errada ou conta desabilitada (ver
  [§3](#login)). Não diferencie esses casos no seu backend nem no seu front. O texto
  `account disabled` não existe mais em nenhuma resposta de login/refresh.
- Rate limit (`429 {"error":"rate limit exceeded"}`) é **por IP + rota**, em memória, no
  próprio janus. Se seu backend é o único chamador (todo tráfego sai do mesmo IP),
  ajuste `RATE_LIMIT_REQUESTS`/`RATE_LIMIT_WINDOW` no ambiente do Janus pra não se
  auto-limitar (é o que o `docker-compose.yml` deste workspace já faz).
- Lockout de conta (`LOCKOUT_THRESHOLD`/`LOCKOUT_DURATION`) é por conta+IP, também em
  memória — mesmo caveat de multi-réplica do rate limit (ver README do Janus,
  seção "Security Notes").

---

## 12. Checklist de integração

- [ ] `JWT_SECRET` idêntico nos dois lados, com 45+ caracteres; cada `SERVICE_KEY` também 45+.
- [ ] `DEV_ENV` **não** definido (ou `false`) no Janus em produção.
- [ ] Service key do Janus só existe no seu **backend** (nunca no front, nunca em
      variável exposta ao browser).
- [ ] Middleware de auth no seu backend valida o JWT localmente (HS256, mesmo segredo) —
      nenhuma chamada ao Janus por request autenticado.
- [ ] Tabela local própria (`Profile` ou equivalente) indexada pelo `sub`/`id` do
      Janus, guardando role + qualquer dado de produto.
- [ ] CRUD de usuário no seu backend sempre faz as duas pontas: Janus (identidade)
      + tabela local (role/dados) — nunca só uma.
- [ ] Login do seu backend devolve **a mesma** resposta (`401 invalid credentials`) para
      todas as falhas, inclusive `Profile.Active=false` — nunca `account disabled`.
- [ ] Desativar usuário = desativar nos dois lados (Janus `disabled_at` + seu
      `Active=false`), sabendo que o access token já emitido some só quando expira.
- [ ] Fluxo de invite guarda o hash do token localmente pra saber qual role aplicar no
      accept (o Janus não tem esse conceito).
- [ ] Endpoint de accept-invite do seu backend é público (sem JWT) mas o resto do
      backend continua exigindo JWT válido.
- [ ] Nenhum claim `roles`/`tenant` é esperado ou lido do JWT — role vem sempre do seu
      banco.

---

## Referência rápida de endpoints do Janus

| Método | Rota | Auth | Uso típico no seu backend |
|---|---|---|---|
| GET | `/v1/health` | nenhuma | healthcheck |
| POST | `/v1/auth/login` | nenhuma | proxy direto |
| POST | `/v1/auth/refresh` | nenhuma | proxy direto |
| POST | `/v1/auth/logout` | Bearer access | proxy direto |
| GET | `/v1/me` | Bearer access | opcional (você já tem o `sub` do seu próprio JWT parse) |
| POST | `/v1/auth/magic-link` | nenhuma | proxy direto |
| POST | `/v1/auth/magic-link/consume` | nenhuma | proxy direto |
| POST | `/v1/auth/2fa/verify` | nenhuma | proxy direto |
| POST | `/v1/me/2fa/enable` | Bearer access | proxy direto |
| POST | `/v1/me/2fa/disable` | Bearer access | proxy direto |
| POST | `/v1/password/forgot` | nenhuma | proxy direto |
| POST | `/v1/password/reset` | nenhuma | proxy direto |
| POST | `/v1/users` | Bearer service_key | + criar `Profile` local |
| GET | `/v1/users?page=N` | Bearer service_key | listar (paginado, 100/pág) / achar por e-mail percorrendo as páginas |
| GET | `/v1/users/:id` | Bearer service_key | detalhe |
| PATCH | `/v1/users/:id` | Bearer service_key | + espelhar em `Profile` local |
| POST | `/v1/invites` | Bearer service_key | + guardar invite local (role, hash) |
| POST | `/v1/invites/accept` | nenhuma | + criar `Profile` local, marcar aceito |

Exemplos completos, testados de ponta a ponta, estão no código de `crazy-back` — em
especial `features/auth/`, `features/admin/`, `features/invite/` e
`internal/janus/client.go` (o cliente HTTP que fala com o Janus).
