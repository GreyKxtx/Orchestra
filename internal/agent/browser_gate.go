package agent

import (
	"errors"
	"strings"
)

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
