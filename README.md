# cnnbr

Leitor de terminal para o [cnnbrasil.com.br](https://www.cnnbrasil.com.br), no
espírito do [circumflex](https://github.com/bensadeh/circumflex): as manchetes
numa lista, o texto inteiro no terminal, tudo pelo teclado.

O conteúdo vem do RSS da CNN, que expõe a matéria completa em
`content:encoded` — não há scraping de página, e o feed em cache faz a lista
abrir instantaneamente (inclusive offline).

## Seções

Uma aba por editoria: Home, Política, Nacional, Internacional, Economia,
Esportes, Pop, Tecnologia, Saúde e Eleições. Cada aba busca o próprio feed
(`/feed/?cat=<id>`, que já inclui as subcategorias) e tem cache e posição de
leitura independentes — o carregamento é preguiçoso, só na primeira visita.

Dentro de uma seção, cada matéria mostra a editoria específica
(`BRASILEIRÃO`, `FUTEBOL`) em vez de repetir o nome da aba.

## Instalação

```sh
go install github.com/levyvix/cnnbr@latest
```

Ou, no clone:

```sh
go build -o cnnbr . && ./cnnbr
```

## Atalhos

| tecla | ação |
| --- | --- |
| `tab` `shift+tab` | próxima / seção anterior |
| `h` `l` `←` `→` | trocar de seção (na lista) |
| `1`…`9` `0` | pular direto para uma seção |
| `j` `k` `↓` `↑` | navegar na lista / rolar a matéria |
| `ctrl+d` `ctrl+u` | meia página |
| `g` `G` | início / fim |
| `enter` | abrir a matéria |
| `esc` `q` | voltar (na lista, sai) |
| `J` `K` | próxima / anterior sem sair do leitor |
| `o` | abrir no navegador |
| `y` | copiar o link |
| `f` | salvar nos favoritos |
| `m` | alternar lida / não lida |
| `s` | ver só os favoritos |
| `t` | alternar texto justificado |
| `r` | recarregar a seção atual |
| `?` | ajuda |

A roda do mouse também rola, tanto na lista quanto no leitor.

## Opções

```
-pages N      páginas do feed a buscar (padrão 2, 60 matérias por página)
-ttl D        validade do cache antes de buscar de novo (padrão 15m)
-justify      abrir já com o texto justificado nas duas margens
```

## Desenvolvimento

```sh
make test   # baixa o feed real para internal/feed/testdata/ e roda os testes
```

Os testes de formatação rodam contra um feed de verdade (todas as matérias, em
várias larguras de terminal). O arquivo não fica no repositório — sem ele, esses
testes são pulados em vez de falhar.

## Onde ficam os arquivos

- cache dos feeds: `$XDG_CACHE_HOME/cnnbr/feed-<seção>.json`
- lidas e favoritos: `$XDG_DATA_HOME/cnnbr/state.json`

Marcações de leitura com mais de 60 dias são descartadas na inicialização;
favoritos ficam para sempre.
