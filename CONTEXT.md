# cnnbr

Leitor de terminal para o feed da CNN Brasil: as manchetes numa lista, o texto
inteiro no terminal, tudo pelo teclado.

## Language

**Seção**:
Um recorte do feed da CNN que o leitor apresenta como uma aba, com cache e
posição de leitura próprios.
_Avoid_: aba, editoria, categoria

**Editoria**:
O rótulo de categoria exibido numa matéria — a categoria raiz na Home
(`ESPORTES`), a subcategoria dentro de uma [[seção]] (`BRASILEIRÃO`).
_Avoid_: subseção, tag

**Motor**:
O sintetizador de voz que fala a matéria — `piper`, `espeak-ng` ou `say` —
escolhido por auto-detecção no `PATH`, não pelo leitor.
_Avoid_: engine, TTS, sintetizador

**Voz**:
O modelo neural pt-BR que o piper carrega (`faber`, `cadu`), baixado sob demanda
e escolhido no painel. Só o piper tem [[voz]]; os outros motores têm uma só.
_Avoid_: modelo, locutor

**Preferência**:
Uma escolha do leitor sobre como o programa se comporta, que sobrevive ao
encerramento. Distinta das dependências de execução que o `main` injeta.
_Avoid_: configuração, ajuste, opção
