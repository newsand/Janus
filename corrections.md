# Correções — revisão adversarial do Janus

- **Revisor:** Moriaty (revisão adversarial, somente leitura)
- **Commit analisado:** `43ff6fe` (`origin/main`, "useless-references")
- **Data:** 2026-10-08
- **Ambiente:** servidor real compilado do repo, ligado a um Postgres 17 local com `migrations/001_initial.sql` aplicada (`DEV_ENV=true`, `MAILER_STUB=true`). Os testes da casa rodam em SQLite em memória.

## Veredito

**VETO.** Os 15 testes passam, mas foram reproduzidas três quebras de segurança, um furo de configuração e a falha de e-mail case-sensitive. Os testes não cobrem os pontos onde os bugs estão (password, 2FA, magic link, middleware, concorrência).

## O que o projeto promete

Serviço de identidade em Go (Gin + GORM + Postgres) que só emite JWT: access de 15 min, refresh opaco de 14 dias com rotação e detecção de reuso, sem roles e sem redirect. Promete login com falha única, lockout por conta+IP, tokens one-shot para recuperação de senha, magic link e convite, 2FA TOTP opcional e CRUD por service key. As regras "fechadas" estão em `AUTH-MVP.md` e `SPEC-DRIVEN.md`; o guia de clientes está em `INTEGRATION.md` e `doc-integracao/`.

---

## 1. E-mail diferencia maiúsculas de minúsculas (Bloqueante)

**Conclusão (verificada):** o e-mail é case-sensitive em todos os pontos. Não há `lower()`, `citext`, collation especial, `strings.ToLower`, `EqualFold` nem `TrimSpace` aplicado ao e-mail (`rg` em todo o Go e SQL). É consistente (grava cru e compara cru), mas o resultado é que uma pessoa pode ter várias contas.

**Onde:**

- Banco: `migrations/001_initial.sql:9` (`email VARCHAR(255) NOT NULL UNIQUE`) e `:21` (índice comum); `internal/models/models.go:15` (`uniqueIndex`). No Postgres a unicidade é por byte. `invites.email` (`001_initial.sql:43`, `:51`) tem o mesmo problema.
- Comparações cruas `email = ?`: criação de usuário `features/users/handler.go:78` (grava em `:84`), PATCH `:213-219`, supersede de convite `:279`, aceite de convite `:334` (cria em `:364`), login `features/auth/handler.go:92`, chave do lockout `features/auth/handler.go:80`, magic link `features/auth/handler.go:237`, recuperação `features/password/handler.go:46`.

**Achados e reproduções:**

- [ ] **Cadastro duplicado.** `POST /v1/users` com `Foo@Bar.com`, depois `foo@bar.com`, depois `FOO@BAR.COM` → **201, 201, 201**; três linhas no banco.
- [ ] **Login depende da capitalização.** `Foo@Bar.com` + senha → 200; `foo@bar.com` + a mesma senha → 401; `fOO@bAR.cOM` → 401.
- [ ] **409 do convite contornável.** Com `Foo@Bar.com` ativo e com senha, convite para `foo@BAR.com` + accept → **200 "account activated"** e uma conta nova com outro `sub`. `TestAcceptInviteRejectsExistingAccountWithPassword` só cobre a capitalização exata.
- [ ] **Usuário pendente não é ativado.** `Pending@Corp.com` criado sem senha; convite para `pending@corp.com` → o accept **cria outra conta**, a pendente continua sem senha.
- [ ] **Convite novo não invalida o antigo.** Convite para `A@x.com` e depois `a@x.com` → os dois ficam válidos (viola AUTH-MVP §7, "novo invite invalida anteriores").
- [ ] **Recuperação e magic link silenciosos.** `forgot` com `FOO@bar.com` e `magic-link` com `foo@bar.COM` → 200 genérico e **nenhum token gerado**. Só o e-mail exato gera token. O usuário nunca recebe o link e não sabe por quê.
- [ ] **Espaços nas pontas.** Create/login/forgot/magic rejeitam `" foo@bar.com "` pelo validador (`binding:"email"`): 400 no create e no login (no login deveria ser o 401 único) e **200 silencioso** no forgot/magic. O PATCH não valida: `{"email":" Foo@Bar.com "}` foi gravado com espaços e a conta ficou **inacessível** (login com espaços → 400, sem espaços → 401). O PATCH também aceitou `"not-an-email"` e `""`.
- [ ] **Lockout (risco latente).** A chave é `"login:"+email` cru (`features/auth/handler.go:80`). Hoje não há bypass porque com outra capitalização o login também não acha o usuário; se alguém normalizar só a busca e esquecer a chave, o lockout passa a ser contornável.

**Correção sugerida:**

- Um helper único `normalizeEmail = strings.ToLower(strings.TrimSpace(email))`, usado em todos os handlers e na chave do lockout.
- Banco: `CREATE UNIQUE INDEX ON users (lower(email))` (ou `citext`), idem para `invites`; migration que detecte e resolva duplicatas existentes.
- PATCH com `binding:"omitempty,email"`.
- Testes de capitalização e espaços rodando contra Postgres.

---

## 2. Bloqueantes

- [ ] **B1. Rate limit contornável via `X-Forwarded-For` → força bruta no 2FA.**
  - Onde: `internal/middleware/ratelimit.go:32` (chave = `c.ClientIP()`); `main.go:42` (`gin.New()` sem `SetTrustedProxies`, então o Gin confia no header vindo de qualquer cliente); `features/twofa/handler.go:76-79` (código errado não queima o challenge).
  - Reprodução (padrão 5 req/min): sem o header, `/v1/password/forgot` → `200×5` e depois `429`. Variando `X-Forwarded-For: 10.0.0.$i` → **30× 200 seguidos**. No verify, 20 códigos errados no mesmo challenge e ele ainda aceitou o código certo. Quem já tem a senha pode tentar TOTP sem limite durante os 5 min do challenge (ataque em escala não executado; é dedução).
  - Efeito colateral: atrás do BFF recomendado em `INTEGRATION.md`, se o header não for repassado, todos os usuários dividem um único balde de 5 req/min por rota.
  - Correção: `r.SetTrustedProxies([...])` com a lista real de proxies; limite de tentativas por challenge (apagar após N erros); rate limit por usuário no verify.

- [ ] **B2. Tokens one-shot não são atômicos (magic link, reset, convite).**
  - Onde: `features/auth/handler.go:277-283`, `features/password/handler.go:95-110`, `features/users/handler.go:328-376` (ler → checar `used_at` → atualizar).
  - Reprodução: 20 chamadas paralelas de `POST /v1/auth/magic-link/consume` com o mesmo token; em 3 rodadas saíram **1, 3 e 3 sessões** do mesmo magic link.
  - Correção: `UPDATE ... SET used_at = now() WHERE token_hash = ? AND used_at IS NULL AND expires_at > now()` e exigir `RowsAffected == 1`, como o refresh já faz.

- [ ] **B3. `MAILER_STUB` é `true` por padrão e não existe mailer real.**
  - Onde: `internal/config/config.go:53`; logs de token em `features/auth/handler.go:256`, `features/password/handler.go:74`, `features/users/handler.go:301`.
  - Evidência: em produção, por padrão, tokens de magic link, recuperação e convite vão crus para o log em INFO (token de recuperação visto no log durante o teste; omitido aqui). Contradiz `CLAUDE.md:45` ("never store or log the raw token"). `POST /v1/invites` também devolve o token no corpo.
  - Com `MAILER_STUB=false` nada é enviado: não há código de SMTP/mailer (`rg` confirma). Mesmo assim `doc-integracao/README.md:416-417` diz que "o Janus entrega o convite por e-mail".
  - Correção: `Validate()` recusar `MAILER_STUB=true` fora de `DEV_ENV`; implementar o mailer; nunca logar token.

---

## 3. Médios

- [ ] **M1. Usuário desabilitado recebe tokens via 2FA.** `features/twofa/handler.go:71` não checa `disabled_at`. Reprodução: login até o challenge → PATCH `disabled_at` → verify → **200 com access e refresh**. Correção: incluir `disabled_at IS NULL` na busca do usuário.
- [ ] **M2. Replay de TOTP.** `features/twofa/handler.go:76` não guarda o último passo usado. Reprodução: o mesmo código foi aceito em dois logins seguidos. Correção: persistir o último time-step aceito e rejeitar código igual ou anterior.
- [ ] **M3. Reset de senha não invalida magic link nem challenges de 2FA.** `features/password/handler.go:107-114` só revoga refresh tokens. Reprodução: pedir magic link → redefinir senha → consumir o link antigo → **200**. Correção: no reset, marcar `magic_tokens` como usados e apagar `two_fa_challenges` do usuário.
- [ ] **M4. Lockout só por conta, não "conta+IP" (AUTH-MVP §1, `README.md:236`).** A chave é só o e-mail (`features/auth/handler.go:80`). Reprodução de DoS: 5 senhas erradas de 5 IPs diferentes → login com a senha certa de um sexto IP → **429**. Correção: alinhar spec e código (chave conta+IP, ou documentar e mitigar o DoS).
- [ ] **M5. Senha acima de 72 bytes → 500.** `features/users/handler.go:95`, `:341`; `features/password/handler.go:100` (inclui endpoints públicos). Reprodução: create com senha de 73 bytes → **500**, log `bcrypt: password length exceeds 72 bytes`. Correção: `binding:"max=72"` (em bytes) e 400.
- [ ] **M6. Logout derruba todas as sessões.** `features/auth/handler.go:204-206` revoga todos os refresh do usuário; AUTH-MVP §4 diz "revoga refresh atual" e §1 põe "sair de todos os devices" fora do escopo. Reprodução: 2 sessões ativas → logout da sessão 1 → **0 ativas**. Correção: decidir; ou a spec muda, ou o logout passa a receber o refresh e revogar só a família dele.
- [ ] **M7. `INTEGRATION.md` desatualizado.** Documenta `401 account disabled` em login e refresh (`INTEGRATION.md:156`, `:638`, `:1010`, `:1037`), resposta que o código e o AUTH-MVP §7 dizem não existir. Faltam `429 rate limit exceeded` e `400 invalid page`. Correção: atualizar as tabelas de erro.
- [ ] **M8. Testes não cobrem onde estão os bugs.** Sem testes para `features/password`, `features/twofa`, magic link, `internal/middleware` e concorrência; os testes rodam em SQLite, rejeitado pelo AUTH-MVP §9. Nenhum teste tautológico encontrado; o problema é cobertura. Correção: testes desses fluxos, testes de corrida e um job contra Postgres.
- [ ] **M9. Mapas em memória crescem sem limite.** `internal/middleware/ratelimit.go`: entradas de `rateLimits` nunca são apagadas; `lockouts` só apaga entradas que chegaram ao limite. Com B1, IPs e e-mails aleatórios fazem a memória crescer. Visto no código, não medido. Correção: expiração/limpeza periódica ou store externo (Redis).

---

## 4. Menores

- [ ] **`disabled_at` aceita qualquer string, inclusive `"false"`.** `features/users/handler.go:241-247`. Reproduzido: `{"disabled_at":"false"}` desabilitou a conta. Correção: aceitar só timestamp/`true` ou um campo booleano.
- [ ] **CPF com mais de 14 caracteres → 500.** Coluna `VARCHAR(14)` (`migrations/001_initial.sql:13`), sem validação em `features/users/handler.go`. Reproduzido. Correção: validar tamanho/formato e devolver 400.
- [ ] **Criação simultânea com o mesmo e-mail → 500.** `features/users/handler.go:77-109` (checa e depois insere). Reproduzido: `1× 201 + 9× 500`. Correção: tratar violação de unicidade como 409.
- [ ] **`/health` responde 200 com banco fora.** `features/health/handler.go:24`. Reproduzido derrubando o Postgres: `{"database":"error"}` com HTTP 200. Correção: 503 quando o banco falhar.
- [ ] **Versão de Go inconsistente.** `README.md:206` pede 1.22+, `go.mod:3` exige 1.26.0, `SPEC-DRIVEN.md` §4 diz ≥ 1.25.
- [ ] **`.env.example:17`** diz "min-32-chars"; a regra é 45.
- [ ] **Renomeação do banco quebra volumes existentes.** O commit `43ff6fe` trocou `loginbuskar` por `janus` em `docker-compose.yml` e `internal/config/config.go`; um volume antigo não tem o banco `janus` e o app não sobe. Análise de código, não executado.
- [ ] **Sem shutdown gracioso.** `main.go:57-68`: o sinal só retorna; requisições em andamento caem.
- [ ] **Versão fixa.** `SPEC-DRIVEN.md` §5 pede git describe ou arquivo VERSION; o código loga "alfa" fixo (`internal/config/config.go:55`).
- [ ] **Caminho errado na spec.** `SPEC-DRIVEN.md` §9 aponta para `../doc-integracao/`; a pasta está na raiz do repo.
- [ ] **Índices redundantes.** `migrations/001_initial.sql`: UNIQUE + `idx_*` nas mesmas colunas (`users.email`, `token_hash` de todas as tabelas).
- [ ] **`issueTokens` duplicado.** `features/twofa/handler.go:201-240` repete `features/auth/handler.go:315-358`; risco de divergência.
- [ ] **Enable de 2FA regenera o secret em silêncio.** `features/twofa/handler.go:107-123`: body malformado ou `{"code":""}` gera secret novo.
- [ ] **Infra.** Sem CI (`.github` não existe). `Dockerfile` roda como root em `alpine:3.19` (suspeita de imagem fora de suporte; não conferido).

---

## 5. Resultado dos testes

- `go test ./...`: **15 testes, todos passaram** (auth 6, users 8, config 1).
- `go vet` limpo; `-race` passou; `-shuffle` com 8 seeds passou.
- Pacotes **sem nenhum teste:** `features/health`, `features/password`, `features/twofa`, `internal/middleware`, `internal/models`, `internal/db`.

## 6. O que não foi verificado

- **Força bruta real no TOTP:** as duas peças foram demonstradas (bypass do rate limit e challenge que não queima), mas o ataque em escala não foi executado.
- **Data race em `CheckLockout`:** lê campos da entrada depois do `RUnlock` (`internal/middleware/ratelimit.go:63-76`). Os testes não são concorrentes, então o `-race` não pega. Suspeita.
- **Integração `crazy-back`:** os arquivos citados em `doc-integracao/` estão fora do repo.
- **Deploy Nixpacks/Railway e `docker compose`:** não testados (sem Docker no ambiente da revisão); usado Postgres nativo com a mesma migration.
- **GitHub:** repo público, sem issues abertas, dois PRs já mergeados, sem branches além da `main` no momento da revisão.

## 7. Ordem de prioridade

1. E-mail case-sensitive (seção 1)
2. B1 — rate limit / `X-Forwarded-For` / força bruta no 2FA
3. B2 — tokens one-shot atômicos
4. B3 — `MAILER_STUB` padrão e mailer inexistente
5. M1 a M5
6. M6 a M9
7. Menores
