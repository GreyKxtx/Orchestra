package agent

import (
	"errors"
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
)

// ModeForRoutedTurn is the mode an agent-mode turn runs in once the router has
// picked routed. The router reads the query only, and ask, explore and plan
// have no browser tools, so a turn given the browser keeps agent mode — build's
// tools — instead of landing where it cannot open a page. kept reports that.
func ModeForRoutedTurn(routed string, allowBrowser bool) (mode string, kept bool) {
	if !allowBrowser || modeOffersBrowser(routed) {
		return routed, false
	}
	return string(ModeAgent), true
}

// modeOffersBrowser reads the mode's tools from the registry rather than
// keeping a second list of which modes have the browser.
func modeOffersBrowser(mode string) bool {
	for _, d := range tools.ListToolsForMode(mode, tools.Capabilities{Browser: true}, true, true) {
		if strings.HasPrefix(d.Function.Name, "browser.") {
			return true
		}
	}
	return false
}

// errBrowserNotAllowed is the refusal for a browser tool in a run that was not
// given the browser.
var errBrowserNotAllowed = errors.New("browser tools are not enabled for this run: " +
	"it was started without allow_browser (--allow-browser on the CLI)")

// browserCallRefusal refuses browser.* in a run without AllowBrowser.
//
// Offering the tools on AllowBrowser is not enough: a model can name a tool it
// was not offered, and the Runner may be shared with runs that do have the
// browser — core keeps one Runner for every session — so its browser client
// existing says nothing about this run's consent. The Runner dispatches browser
// tools by exact "browser." names, which the prefix covers.
func (a *Agent) browserCallRefusal(name string) error {
	if a.opts.AllowBrowser || !strings.HasPrefix(name, "browser.") {
		return nil
	}
	return errBrowserNotAllowed
}
