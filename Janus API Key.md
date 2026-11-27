Janus · estudo de extensão

# Uma rota, dois caminhos de identidade

O sistema integrado e o navegador batem na mesma rota do backend do produto. O prefixo do token decide o caminho: `jns_` é API key e é verificada no Janus pela intranet; `eyJ` é JWT e é validado ali mesmo, sem chamar ninguém. O Janus nunca é exposto à internet.

Caminho A · máquina

## API key, verificada no Janus

Introspecção a cada chamada (ou a cada TTL do cache). Revogação vale na hora.

intranet

**Sistema integrado**terceiro, na internet

**Backend do produto**autoriza e serve os dados

**Janus**só identidade

1Requisição com a chave

`GET /pedidos · Bearer jns_<64-hex>`

2Prefixo `jns_` → caminho API keyconsulta cache por SHA-256 da chave

3Verifica a chave

`POST /v1/api-keys/verify · service key`

4SHA-256 + busca por índice únicorevogada, expirada ou dono desativado → inválida

5Identidade, sem permissões

`200 {valid, key_id, owner_user_id, audience}`

6Cache 30–60 s · principal `{tipo:"api_key"}`o produto decide o que a chave pode ver

7Dados

`200 [ …pedidos ]`

Custo

Um hash e um `SELECT` indexado: menos de 1 ms no Janus, mais o round-trip na intranet.

Janus fora

Sem cache, responder `503`. Falhar fechado: nunca liberar por padrão.

Falha

Qualquer motivo de chave inválida vira o mesmo `401 invalid api key`.

Caminho B · usuário

## JWT, validado no próprio backend

Assinatura HMAC e expiração conferidas localmente. O Janus só entra no login e no refresh, a cada 15 min.

intranet

**Navegador**usuário logado

**Backend do produto**mesma rota

**Janus**não consultado

1Requisição com o access token

`GET /pedidos · Bearer <access_token>`

2Prefixo `eyJ` → caminho JWTHMAC com `JWT_SECRET` + `exp`, sem rede

3Principal `{tipo:"user", id: sub}`mesma estrutura do caminho A

4Dados

`200 [ …pedidos ]`

Onde os dois se encontram

### Um middleware, um principal

- Mesmo header `Authorization`; o prefixo separa os caminhos.
- Os dois viram `{tipo, id, owner_user_id}` antes do handler.
- Rotas podem aceitar só `user`, só `api_key` ou os dois.

Disponibilidade

### O Janus já é obrigatório

- Sem refresh, todo usuário cai em até 15 min.
- JWT degrada aos poucos; API key sem cache cai na hora.
- O cache curto dá às integrações a mesma folga do JWT.

O que o Janus não faz

### Identidade, nunca permissão

- Sem escopos nem roles na resposta do `verify`.
- Chave de integração age em nome do dono, não como ele.
- Terceiros nunca falam com o Janus.