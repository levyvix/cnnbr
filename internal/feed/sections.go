package feed

// Section é uma aba do leitor: um recorte do feed da CNN.
//
// A CNN roda WordPress, e /feed/?cat=<id> devolve o RSS já filtrado pela
// categoria — incluindo as subcategorias, então "Esportes" traz Brasileirão,
// Fórmula 1 e o resto. Os slugs em /politica/feed/ NÃO servem: devolvem a
// página HTML, não RSS.
type Section struct {
	Slug string // usado como chave de cache
	Name string // rótulo da aba
	Cat  int    // ID da categoria no WordPress; 0 = feed geral
}

// Sections são as abas exibidas, na ordem. Os IDs vêm de
// /wp-json/wp/v2/categories?parent=0 — as categorias raiz da CNN.
var Sections = []Section{
	{Slug: "home", Name: "Home", Cat: 0},
	{Slug: "politica", Name: "Política", Cat: 482},
	{Slug: "nacional", Name: "Nacional", Cat: 1479},
	{Slug: "internacional", Name: "Internacional", Cat: 903},
	{Slug: "economia", Name: "Economia", Cat: 1116},
	{Slug: "esportes", Name: "Esportes", Cat: 123},
	{Slug: "pop", Name: "Pop", Cat: 1630},
	{Slug: "tecnologia", Name: "Tecnologia", Cat: 1930},
	{Slug: "saude", Name: "Saúde", Cat: 1438},
	{Slug: "eleicoes", Name: "Eleições", Cat: 55743},
}
