# LoginBuskar / Janus — Spec-Driven Design (pré-código)

**Repo:** https://github.com/newsand/janus  
**Produto:** serviço de identidade JWT (sem redirect, sem roles)  
**Canônico de produto:** `AUTH-MVP.md` (mesma pasta / raiz do repo)

## 1. Problema

Fronts (Buskar, Vistoria, etc.) precisam autenticar usuários sem OAuth bounce. Um serviço único prova identidade e emite JWT; autorização fica nos produtos.

## 2. Fora / dentro

Ver `AUTH-MVP.md` §1. Resumo: login, refresh (rotação+reuse), logout, recover, magic link, invite, CRUD via service key, 2FA on/off opcional, rate limit/lockout, Postgres. Sem roles, sem signup aberto, sem redirect.

## 3. Arquitetura (obrigatória — enxuta)

**Proibido:** hexagonal, clean architecture, DDD, camadas repository/service genéricas, pastas `domain`/`usecase`/`infrastructure`.

**Permitido / desejado:**

- Separar por **feature** (`features/auth`, `features/users`, `features/health`, …), não por camada.
- Um `main.go` fino: carrega config, inicia singletons, registra rotas, sobe HTTP.
- **Logger singleton** — sem lib de log; `fmt`/`print` colorido no stdio; `LOG_LEVEL` env; `gin.ForceConsoleColor()`.
- **DBManager singleton** — Postgres; padrão Gin de config/DB; pool único.
- **GORM Active Record; no ORM/DB swap abstractions.**
- Rotas com **Router groups** do Gin (`/v1/auth`, `/v1/users`, …).
- Complexidade sobe só quando uma feature exige.

## 4. Stack

- Go **≥ 1.25** (versão mais recente estável disponível no build)
- Gin
- GORM (Postgres driver)
- Postgres
- Deploy: **Nixpacks** (homolog/prod)
- Local/test: **docker compose** = app + Postgres

## 5. Runtime

- No startup: logar **versão do código** (ex. git describe / `VERSION` file).
- `GET /health` (ou `/v1/health`): liveness + **version = alfa** (string explícita `alfa` / `alpha` conforme README).
- Env: `PORT`, `DATABASE_URL`, `LOG_LEVEL`, `DEV_ENV`, `JWT_SECRET`, `SERVICE_KEYS` (dual), TTLs, mailer stub, bootstrap opcional.
- **Segredos na inicialização (fechado):** com `DEV_ENV=true` (modo dev) nada é exigido — valores padrão e chaves curtas valem, para teste local. Com `DEV_ENV` ausente/vazio/`false` (**padrão = produção**) o serviço **não sobe** se `JWT_SECRET` tiver menos de **45 caracteres** ou se `SERVICE_KEYS` estiver vazio ou tiver qualquer chave com menos de 45 caracteres. Sem valor padrão aceito em produção (o antigo `change-me-in-production` só funciona em dev).

## 6. Entregáveis no repo

1. `docs/` ou raiz: `AUTH-MVP.md` + este `SPEC-DRIVEN.md`
2. Código Go conforme §3–5 implementando o MVP
3. `README.md` = **manual de deploy e uso** (não essay de arquitetura)
4. `docker-compose.yml` (postgres + app)
5. Nixpacks (`nixpacks.toml` / o que for necessário) para homolog/prod
6. `.env.example`

## 7. Critério de pronto

- `docker compose up` sobe app+Postgres; health retorna versão alfa
- Login/refresh/logout/recover/magic/invite/CRUD(service key) batem com `AUTH-MVP.md`
- Sem pastas/camadas proibidas; features only
- Moriaty ataca o código contra `AUTH-MVP.md`

## 8. Decisões pós-MVP (registradas aqui para não parecer spec drift)

- **`POST /v1/invites` devolve o token no corpo quando `MAILER_STUB=true`** (`token` +
  `expires_at`, além de `message`/`email`). O invite continua sendo entregue OOB por
  contrato (§7 do `AUTH-MVP.md`); a diferença é só que, sem mailer real configurado, o
  serviço precisa devolver o valor pra quem chamou poder entregá-lo de outro jeito (ex.:
  montar o link do magic-link). Com `MAILER_STUB=false` o campo `token` não existe na
  resposta — o token volta a ser estritamente OOB, hash-only no DB como sempre foi.

- **`GET /v1/users` é paginado, 100 por página** (`?page=N`, a partir de 1). Ordenação
  `created_at DESC, id DESC` (id desempata para as fronteiras de página ficarem estáveis).
  A resposta mantém `users` e acrescenta `page`, `page_size`, `total`, `total_pages`.
  `page` inválido → `400`; página além da última → lista vazia. Substitui o antigo corte
  fixo nas 100 contas mais recentes (que não estava especificado). Não há busca por
  e-mail: quem precisa achar um e-mail percorre as páginas.

- **Login tem uma única falha de autenticação** (`401 invalid credentials`) para usuário
  inexistente, sem senha, senha errada e conta desabilitada; `account disabled` saiu da
  resposta de login (e do refresh, que devolve `invalid refresh token`). Sempre um bcrypt
  por tentativa (hash dummy de mesmo custo quando não há hash real). Senha errada conta no
  lockout, inclusive em conta desabilitada; senha certa em conta desabilitada não emite
  token nem mexe no lockout. Contrato completo em `AUTH-MVP.md` §7 ("Login — falha de
  autenticação é uma só"). Substitui o comportamento anterior, em que `account disabled`
  saía antes da senha e fora do lockout.

- **`nome` é aparado e nunca vazio.** `POST /v1/users` e `PATCH /v1/users/:id` aplicam
  `strings.TrimSpace` antes de validar (`binding:"required"` sozinho só barra `""`, deixava
  `"   "` passar, e o PATCH aceitava até vazio). Vazio ou só espaços → `400`; o valor gravado
  é o aparado. No accept do convite, nome vazio após o trim cai para o e-mail.

- **Trocar `email` via `PATCH /v1/users/:id` revoga todos os refresh tokens do usuário**
  (igual ao bloqueio por `disabled_at`). Motivo: o e-mail é identidade/canal de recuperação;
  sessões abertas com o e-mail antigo não devem sobreviver à troca. `nome`/`telefone`/`cpf`
  não revogam. Não há endpoint avulso de revogação — o bloqueio por segurança é o mecanismo.
  Access JWT já emitido vale até expirar (limitação aceita). Teste:
  `TestUpdateUserEmailChangeRevokesRefreshTokens`.

- **`DEV_ENV` + validação de segredos.** Antes o `JWT_SECRET` caía em `change-me-in-production` e o compose injetava `key1,key2` sem aviso: um deploy esquecido subia com segredo público (qualquer um forjaria JWT / usaria a service key). Agora o padrão é produção estrita; ver §5. Só o Janus; o crazy-back não foi alterado.

## 9. Integração por produtos consumidores

Todo produto que usa este serviço para identidade (roles/dados ficam no produto, nunca
aqui) deve seguir o guia em `../doc-integracao/`, que documenta o padrão de integração
usado pelo `crazy-back`/`crazy-front` (JWT com segredo compartilhado, service key só no
backend do produto, fluxo de invite/magic-link ponta a ponta).
