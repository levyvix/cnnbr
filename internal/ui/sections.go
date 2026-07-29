package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/levyvix/cnnbr/internal/prefs"
)

// Este arquivo é o grupo "Seções" do painel: quais seções aparecem na barra de
// abas e em que ordem. O layout do painel fica em panel.go.

// orderPos é a posição de uma aba na ordem escolhida.
func (m Model) orderPos(idx int) int {
	for pos, at := range m.order {
		if at == idx {
			return pos
		}
	}
	return 0
}

// toggleSection mostra ou oculta uma seção. Nada é jogado fora: a aba continua
// no modelo, com o que já carregou.
func (m Model) toggleSection(idx int) (tea.Model, tea.Cmd) {
	t := &m.tabs[idx]
	if !t.hidden && len(m.visible) == 1 {
		return m, statusErr("a última seção visível não pode ser ocultada")
	}

	t.hidden = !t.hidden
	m.rebuildVisible()
	m.recordSections()

	if t.hidden && idx == m.active {
		// A aba ativa sumiu da barra: cai na visível mais próxima, sem fechar o
		// painel. A busca é a da primeira visita — reexibir nunca busca.
		m.active = m.nearestVisible(idx)
		m.rebuildView(m.active)
		return m, m.loadActive(false)
	}
	return m, nil
}

// nearestVisible é a aba visível mais próxima de `idx` na ordem escolhida:
// a seguinte, ou a anterior quando não há seguinte.
func (m Model) nearestVisible(idx int) int {
	pos := m.orderPos(idx)
	for i := pos + 1; i < len(m.order); i++ {
		if !m.tabs[m.order[i]].hidden {
			return m.order[i]
		}
	}
	for i := pos - 1; i >= 0; i-- {
		if !m.tabs[m.order[i]].hidden {
			return m.order[i]
		}
	}
	return m.active
}

// moveSection desloca a seção sob o cursor uma posição na ordem das abas. Nas
// pontas não faz nada: dar a volta ao mover é fácil de disparar por engano.
func (m *Model) moveSection(delta int) {
	row := m.panelRows()[m.panel.cursor]
	if row.section == nil {
		return
	}

	pos := m.orderPos(*row.section)
	next := pos + delta
	if next < 0 || next >= len(m.order) {
		return
	}

	m.order[pos], m.order[next] = m.order[next], m.order[pos]
	m.panel.cursor += delta // o cursor acompanha a seção movida
	m.rebuildVisible()
	m.recordSections()
	m.clampPanel()
}

// recordSections guarda a lista completa de seções, na ordem e com o marcador
// de visibilidade — não só as visíveis. Ver docs/adr/0002.
func (m *Model) recordSections() {
	list := make([]prefs.Section, 0, len(m.order))
	for _, idx := range m.order {
		list = append(list, prefs.Section{
			Slug:    m.tabs[idx].section.Slug,
			Visible: !m.tabs[idx].hidden,
		})
	}
	m.prefs.Sections = list
	m.chosen.Sections = list
}
