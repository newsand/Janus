# MASTER — design system do guia interativo do Janus

Fonte única de verdade visual de `doc-integracao/index.html` (o guia estático em HTML, abre com
duplo clique, sem servidor). Todo valor de cor, tipo, espaço, forma e movimento do guia sai
daqui; nada de hex ou px solto no CSS.

Direção de arte: a pintura em `../janus/assets/cover.jpg` (Janus de duas faces, chave, louro,
mármore, ouro e bordô). O guia tem dois modos sobre o **mesmo** conteúdo.

## 1. Teses (validadas)

**Visual.** Dois mundos de um mesmo deus. *Modo Dev*: interior de mármore à noite — fundo umbra
quase preto, ouro antigo como único acento, bordô só nas superfícies de ação; títulos em
serifada romana de alto contraste (Cormorant Garamond) sobre texto técnico em IBM Plex Sans e
Plex Mono; espaço amplo; cantos quase retos como pedra lavrada; a pintura do Janus como porta de
entrada. *Modo IA*: a ficha do mesmo mundo em pedra clara e tinta — fundo cinza-pedra, texto
grafite, ouro escuro só em filetes, tudo em IBM Plex; denso, sem imagem, sem sombra; cada passo
numerado e rotulado, cada payload em bloco de código, todo o conteúdo escrito no próprio HTML.

**Interação.** *Modo Dev*: médio e solene (220–560 ms), ease-out longo. Hover desenha um filete
dourado em 220 ms, sem escalar nada. A troca de modo é uma porta (dois painéis, 560 ms). Os
passos do fluxo entram por clique, em stagger de 70 ms, nunca por rolagem. *Modo IA*: sem
movimento (0 ms), só foco visível. `prefers-reduced-motion` zera as duas camadas.
Proibido: bounce/elástico, parallax, scroll-jacking, loops automáticos, brilho/partículas,
animar `width`/`height`/`top`/`left`/`margin`/`padding`.

## 2. Contrato de leitura por IA

- O arquivo é lido como **texto**: todo o conteúdo do Modo IA está escrito no HTML (sem depender
  de JavaScript). O Modo Dev é camada visual por cima do **mesmo** markup (progressive
  enhancement): sem JS, a página já aparece no Modo IA.
- Fluxos são `<ol class="flow">`; cada `<li>` leva `data-from`, `data-to`, `data-method`,
  `data-path`; payloads são `<pre>` com `data-kind="request|response|error"`.
- Um `<script type="application/json" id="janus-flows">` resume cada fluxo (passos, status,
  erros) para quem só quer a estrutura.
- Um comentário no topo do arquivo diz onde está o conteúdo canônico.

## 3. Tokens

### 3.1 Cor — Modo Dev (escuro)

| Token | Valor | Uso | Contraste |
|---|---|---|---|
| `--d-bg` | `#0F0C0D` | fundo (umbra) | — |
| `--d-surface` | `#1A1416` | painéis | — |
| `--d-surface-2` | `#241B1E` | cards, blocos elevados | — |
| `--d-line` | `#3A2C30` | bordas, divisórias | — |
| `--d-fg` | `#EDE4D3` | texto | 15,4:1 no fundo |
| `--d-muted` | `#B3A590` | texto de apoio | 8,1:1 no fundo |
| `--d-gold` | `#D4AF5A` | acento único, foco, filetes | 9,3:1 no fundo |
| `--d-burg` | `#8E2433` | superfícies de ação | texto `--d-fg`: 6,8:1 |
| `--d-burg-text` | `#E07A8A` | erro, método de escrita | 6,8:1 no fundo |
| `--d-sky` | `#8FA7C2` | dados, strings em código | 7,9:1 no fundo |

### 3.2 Cor — Modo IA (claro)

| Token | Valor | Uso | Contraste |
|---|---|---|---|
| `--i-bg` | `#EEF0F2` | fundo (pedra) | — |
| `--i-surface` | `#FFFFFF` | blocos de código, tabelas | — |
| `--i-line` | `#C9CED4` | bordas | — |
| `--i-fg` | `#16181C` | texto | 15,6:1 no fundo |
| `--i-muted` | `#4A5059` | texto de apoio | 7,1:1 no fundo |
| `--i-gold` | `#80611A` | filetes, rótulos | 5,1:1 no fundo |
| `--i-burg` | `#7A1E2C` | chaves, métodos | 9,0:1 no fundo |
| `--i-ok` | `#1F6B4A` | sucesso (2xx) | 6,4:1 no papel |
| `--i-err` | `#A4262C` | erro (4xx/5xx) | 7,3:1 no papel |

Regra: texto comum ≥ 4,5:1; texto grande e bordas de interface ≥ 3:1. Nenhum token novo sem
calcular a razão.

### 3.3 Tipografia

| Papel | Família | Uso |
|---|---|---|
| `--f-display` | Cormorant Garamond 500/600 | títulos do Modo Dev |
| `--f-body` | IBM Plex Sans 400/500/600 | texto, rótulos, títulos do Modo IA |
| `--f-mono` | IBM Plex Mono 400/500 | código, endpoints, números |

Fallbacks: display `Georgia, serif`; body `system-ui, sans-serif`; mono `ui-monospace, monospace`.

| Escala | Dev | IA |
|---|---|---|
| Display | 64/1,02 Cormorant 600 | — |
| H1 | 52/1,05 Cormorant 600 | 26/1,2 Plex Sans 600 |
| H2 | 34/1,1 Cormorant 600 | 20/1,25 Plex Sans 600 |
| H3 / rótulo | 12/1,3 Plex Sans 600, caixa alta, `letter-spacing: .16em` | igual |
| Corpo | 16/1,65 | 15/1,6 |
| Código | 13,5/1,6 Plex Mono | 13/1,55 Plex Mono |

Medida de leitura: 65 caracteres (`max-width: 65ch`). `font-variant-numeric: tabular-nums` em
tabelas e códigos de status.

### 3.4 Espaço, forma, profundidade

- Espaço (base 4): `--s1` 4 · `--s2` 8 · `--s3` 12 · `--s4` 16 · `--s5` 24 · `--s6` 32 ·
  `--s7` 48 · `--s8` 64 · `--s9` 96.
- Cantos: `--r-sm` 2 · `--r-md` 4 · `--r-lg` 10 (só Dev, só em elementos grandes). Modo IA: 2 e 4.
- Sombra: Dev `--shadow-stone: 0 18px 40px -18px rgba(0,0,0,.8)` e filete interno
  `inset 0 1px 0 rgba(212,175,90,.25)` nos cards. IA: nenhuma, só borda de 1 px.
- Camadas (`z-index`): conteúdo 0 · barra 10 · menu 20 · porta (troca de modo) 50.
- Largura útil: 1120 px; navegação lateral 260 px; gutter mínimo 16 px.

### 3.5 Movimento

| Token | Valor | Uso |
|---|---|---|
| `--ease-out` | `cubic-bezier(.2,.8,.2,1)` | entradas, hover |
| `--ease-door` | `cubic-bezier(.7,0,.2,1)` | troca de modo |
| `--t-hover` | 220 ms | filete dourado |
| `--t-step` | 360 ms | entrada de passo/payload |
| `--t-stagger` | 70 ms | entre passos |
| `--t-door` | 560 ms | porta (fecha 0–280, troca em 280, abre 280–560) |

Modo IA: todas as durações valem 0. `prefers-reduced-motion: reduce`: durações 0 nos dois modos,
a porta vira troca instantânea e os passos aparecem de uma vez.

## 4. Componentes (cinco estados cada)

Estados: **default · hover · focus-visible · active · disabled**. Foco sempre visível: contorno
de 2 px (`--d-gold` no Dev, `--i-gold` na IA) com `outline-offset: 3px`. Alvos clicáveis ≥ 44×44
px em telas de toque. `cursor: pointer` em todo clicável. Nenhum emoji como ícone: SVG inline.

- **Botão primário.** Dev: fundo `--d-burg`, texto `--d-fg`, borda `--d-gold`; hover clareia o
  bordô (`#A22A3C`); active desloca 1 px para baixo (`translateY(1px)`); disabled 45 % de
  opacidade, sem hover. IA: fundo `--i-fg`, texto `--i-bg`.
- **Botão fantasma.** Fundo transparente, borda `--*-line`; hover troca a borda para o acento.
- **Pílula de modo (Dev | IA).** Dois segmentos, `aria-pressed`; ativo com borda de acento.
- **Link com filete.** Sublinhado de 1 px que desenha da esquerda em `--t-hover`.
- **Card de endpoint.** Selo do método (`POST`/`GET`/`PATCH`/`DELETE`) + caminho em mono +
  descrição. Dev: `--d-surface-2` com filete dourado interno. IA: papel branco com borda.
- **Bloco de código.** Fundo `--*-bg`/`--*-surface`, borda de 1 px, rolagem horizontal própria,
  botão "copiar" no canto (cai para seleção do texto se a área de transferência recusar).
  Sintaxe: chave = ouro/bordô, string = azul/verde, erro = bordô claro/vermelho.
- **Passo de fluxo.** Número, selo do método, caminho, atores (origem → destino). Dev: entra em
  `--t-step`; IA: lista numerada comum.
- **Diagrama de sequência.** Três raias fixas (Navegador · Seu backend · Janus), setas
  desenhadas em SVG inline; o passo ativo ganha traço dourado. Setas sólidas = requisição,
  tracejadas = resposta.
- **Tabela.** Cabeçalho em rótulo caixa alta; linhas separadas por filete; rolagem horizontal
  própria no celular.
- **Checklist.** Caixas marcáveis; estado guardado em `localStorage` com `try/catch` (a página
  funciona sem ele).
- **Aviso.** Borda lateral não é usada; um bloco com rótulo ("Cuidado", "Regra de ouro") em
  caixa alta e filete superior de acento.
- **Busca.** Campo no topo filtra o índice lateral e destaca seções; atalho `/` foca o campo.

## 5. Estrutura do guia (`index.html`)

1. Barra fixa: marca (chave em SVG) · busca · pílula Dev | IA · botão "Copiar contexto para IA".
2. Entrada (só Dev): pintura do Janus em destaque e a regra de ouro.
3. Índice lateral (sticky no desktop; gaveta no celular).
4. Seções 1–12 do `README.md` + referência de endpoints, nesta ordem, com os fluxos como `ol.flow`.
5. Rodapé com links para `README.md`, `MASTER.md` e as specs do Janus.

Fluxos com diagrama/stepper: login, refresh, convite (criar + aceitar), magic link de login,
recuperação de senha. Seções de referência (config, CRUD, erros, checklist, endpoints) usam
tabelas e blocos de código.

## 6. Acessibilidade e responsividade

- Landmarks (`header`, `nav`, `main`, `footer`), títulos em ordem, `aria-pressed` na pílula,
  `aria-live="polite"` no anúncio do passo ativo, `aria-hidden` nos elementos só decorativos.
- Tela de 375 / 768 / 1024 / 1440 px sem rolagem horizontal da página; tabelas e código rolam
  dentro do próprio contêiner.
- Página completa em repouso: nada de conteúdo que só aparece por rolagem; no Dev, apenas o
  passo ativo de um fluxo tem entrada animada e os demais já estão visíveis.
- Sem JavaScript: aparece o Modo IA completo.

## 7. Fora de escopo

2FA e a infraestrutura de proxy/rate limit seguem o texto atual do `README.md`: o guia
reproduz o que o `README.md` diz e não acrescenta nada a respeito.
