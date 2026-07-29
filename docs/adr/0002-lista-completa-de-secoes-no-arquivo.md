# 0002 — A lista completa de seções no arquivo, não uma allowlist

Aceita.

## Contexto

O painel passou a mostrar, ocultar e reordenar as seções. Isso precisa
sobreviver ao encerramento, então vai para o `config.json` — e a forma óbvia
seria guardar só o que interessa: a lista das seções visíveis, na ordem.

## Decisão

O arquivo guarda **todas** as seções, na ordem, cada uma com um marcador de
visibilidade:

```json
"sections": [
  { "slug": "home", "visible": true },
  { "slug": "pop", "visible": false }
]
```

A reconciliação com os slugs que o binário conhece (`prefs.ReconcileSections`)
acontece na leitura:

- Slug que o binário não conhece — seção que a CNN aposentou: ignorado.
- Seção que o binário conhece e o arquivo não menciona — seção nova: entra no
  fim da lista, **visível**.
- A ordem do arquivo é respeitada para o que ele menciona.

## Por que não uma allowlist

Com uma lista só das visíveis, uma seção acrescentada por uma versão futura do
cnnbr não estaria nela e **nasceria oculta**. Você atualizaria o programa, a
seção nova não apareceria em lugar nenhum, e não haveria nada na tela sugerindo
que ela existe — o painel lista o que o arquivo diz. O bug se parece com "essa
versão não tem a seção", que é o diagnóstico errado.

Guardar a lista completa inverte o padrão: o desconhecido aparece. E deixa o
arquivo legível para quem for editá-lo à mão — as dez seções estão lá, com o
marcador ao lado, em vez de um conjunto do qual se deduz o complemento.

Olhando só a struct isso é invisível: `[]Section{Slug, Visible}` e
`[]string` de slugs visíveis resolvem igualmente bem o caso de hoje. A diferença
só aparece na versão seguinte.

## Consequências

- O arquivo cresce com o número de seções (dez linhas de JSON). Irrelevante para
  um arquivo que é lido uma vez no arranque.
- Um arquivo editado à mão pode ocultar todas as seções. A reconciliação reexibe
  a primeira: com zero abas não há o que desenhar.
- Slug repetido no arquivo entra uma vez só, senão a mesma seção viraria duas
  abas com o mesmo cache.
- O slug desconhecido é ignorado na leitura e **some do arquivo** na primeira
  gravação: o que o painel grava é a lista reconciliada. Quem alterna entre duas
  versões do cnnbr perde a posição e a visibilidade de uma seção que só a mais
  nova conhece — mas ela volta visível, no fim da lista, que é o lado seguro do
  erro.
