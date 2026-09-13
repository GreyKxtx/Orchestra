package agent

// A blocked duplicate has to tell the model where to go next, and the advice
// has to differ by tool.
//
// "Use edit/write to apply changes or call with different arguments" is right
// for a repeated bash or fs.rename. Sent in answer to a repeated write it is
// an instruction to repeat the write, and a model that follows it spends its
// remaining denial budget doing so — which is how a fix_compile_error run
// ended with three refused writes and no answer, on a file that needed no
// further change at all.
func duplicateCallRefusal(name string) string {
	if name == "write" || name == "edit" {
		return "⛔ «" + name + "» was already called with these exact arguments, so the file " +
			"already holds exactly this content — the change is applied and sending it again " +
			"does nothing. Do not repeat this call. If the work is done, answer with " +
			`{"patches":[]}. If something is still missing, read the file and work from what ` +
			"it actually contains."
	}
	return "⛔ The tool «" + name + "» was already called with these exact arguments — " +
		"duplicate blocked. Use edit/write to apply changes or call with different arguments."
}
