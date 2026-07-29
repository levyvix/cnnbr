# 0001 — Preferências em arquivo próprio, resolvidas em três camadas

Aceita.

## Contexto

Até aqui toda escolha do leitor vivia em flags de CLI e morria ao sair:
apertar `t` desjustificava o texto, e na execução seguinte a justificação
ressuscitava. Precisávamos de um lugar para as **preferências** — as escolhas do
leitor que sobrevivem ao encerramento, distintas das dependências de execução
que o `main` injeta (cliente HTTP, cache, store).

## Decisão

### Arquivo próprio, não dentro do `state.json`

As preferências vão para `$XDG_CONFIG_HOME/cnnbr/config.json`, separadas do
`$XDG_DATA_HOME/cnnbr/state.json`.

São coisas de natureza diferente. O `state.json` é *dado*: cresce sozinho a cada
matéria aberta, é podado periodicamente, e ninguém abre num editor. O
`config.json` é *escolha*: pequeno, estável, e o caminho normal para editá-lo é o
editor de texto — daí o JSON indentado, a duração como string (`"15m0s"`, porque
`time.Duration` em JSON serializa como nanossegundos e ninguém digita
`900000000000` à mão) e a retenção do histórico em dias (`60`, não `"1440h"`).

Isso também é o que o XDG pede: `XDG_CONFIG_HOME` para configuração,
`XDG_DATA_HOME` para dados. Misturar os dois significaria que apagar o histórico
apagaria as preferências junto.

### Três camadas: flag > arquivo > embutido

A resolução é uma função **pura** (`prefs.Resolve`) que empilha os padrões
embutidos, depois o que veio do arquivo, depois as flags. Duas camadas não
bastavam: sem os padrões embutidos como camada própria, um arquivo parcial
(escrito à mão, ou por uma versão anterior) deixaria campos no valor zero — zero
páginas, TTL de zero.

Sendo pura, a precedência inteira é testável em tabela, sem tocar em disco nem em
`os.Args`; o `main` fica só com a travessia das flags e a chamada.

### Inspecionar as flags digitadas, não os padrões do `flag`

`flag.Bool("justify", true, …)` não distingue "o usuário passou `-justify=false`"
de "o usuário não passou nada": nos dois casos o ponteiro aponta para um valor, e
no segundo esse valor é o padrão. Comparar o valor com o padrão também não
resolve — `-justify=true` explícito é indistinguível da ausência.

Sem resolver isso, o arquivo nunca conseguiria desligar a justificação: o padrão
`true` da flag sobreporia o `false` do arquivo em toda execução.

A saída é `flag.Visit`, que percorre **só as flags realmente digitadas**. O que
ele entrega vira a camada de flags, e os padrões do `flag` passam a ser os
padrões embutidos das preferências — o `-h` continua honesto, mas o valor deles
nunca é usado como override.

## Consequências

- O arquivo só nasce quando algo é gravado. Ausência é o caso normal da primeira
  execução e é silenciosa.
- Arquivo inválido não derruba o programa: caem os padrões embutidos e o aviso
  aparece na **barra de status**. Escrever no stderr não serviria — com a tela
  alternativa do bubbletea, a mensagem só apareceria depois que o programa sai.
- Uma flag é o valor com que o programa nasce, não uma tranca: `t` já sobrepõe
  `-justify` na sessão, e o painel de preferências seguirá o mesmo caminho.
- A poda do histórico usa a retenção das preferências; `0` dias significa "nunca
  podar", e o `main` pula a chamada em vez de passar duração zero (que apagaria
  tudo).
