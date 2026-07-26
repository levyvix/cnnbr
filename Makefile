FEED := https://admin.cnnbrasil.com.br/feed/
FIXTURE := internal/feed/testdata/feed.xml

.PHONY: build test testdata clean

build:
	go build -o cnnbr .

test: testdata
	go test ./...

# Os testes rodam contra um feed real. O arquivo não vai para o repositório
# (600 KB de matérias da CNN), então é baixado sob demanda.
testdata: $(FIXTURE)

$(FIXTURE):
	@mkdir -p $(dir $@)
	curl -sL -A "Mozilla/5.0 (X11; Linux x86_64)" $(FEED) -o $@

clean:
	rm -f cnnbr $(FIXTURE)
