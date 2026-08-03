// Package speech fala a matéria aberta com um motor local: piper com voz
// neural, espeak-ng como rede de segurança, `say` no macOS. Sem rede, sem chave
// de API, sem custo — o texto inteiro já chega no feed.
package speech

import "github.com/levyvix/cnnbr/internal/article"

// spoken são os tipos de bloco que valem em voz alta.
//
// Fora ficam ListItem, Caption, Related, Rule e Embed, e isso foi medido sobre
// as 60 matérias do feed de teste, não chutado: os ListItem são quase sempre
// escalação de futebol e ficha técnica ("TV: Premiere", "Horário: 19h30"), que
// em voz alta são um minuto de suplício; Caption termina em crédito de foto; e
// Embed é literalmente a palavra "vídeo".
//
// A consequência é assumida e não é bug: a versão falada é um resumo da tela.
// Quem ouvir uma matéria de futebol não saberá em que canal ela passa.
var spoken = map[article.Kind]bool{
	article.Heading:    true,
	article.Subheading: true,
	article.Paragraph:  true,
	// Quote entra por ser narrativa, mesmo não ocorrendo em nenhuma das 60.
	article.Quote: true,
}

// Lines é o que se fala, uma linha por bloco. O título abre, porque ouvir uma
// matéria sem saber qual é não ajuda ninguém.
//
// Uma linha por bloco não é enfeite: o piper lê o stdin linha a linha e
// sintetiza a seguinte enquanto a anterior toca.
func Lines(title string, blocks []article.Block) []string {
	var lines []string
	if title != "" {
		lines = append(lines, title)
	}
	for _, b := range blocks {
		if spoken[b.Kind] && b.Text != "" {
			lines = append(lines, b.Text)
		}
	}
	return lines
}
