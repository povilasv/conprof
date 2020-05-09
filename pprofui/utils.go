package pprofui

import (
	"html/template"
	"net/http"
	"strings"
)

func traceUIhttpMain(w http.ResponseWriter, r *http.Request, ranges []Range, path, timestamp string) {
	type Templ struct {
		Range []Range
		Path  string
	}

	if err := templMain.Execute(w, Templ{
		Range: ranges,
		Path:  strings.Join([]string{path, timestamp}, "/"),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

var templMain = template.Must(template.New("").Parse(`
<html>
<body>
{{if $}}
	{{range $e := .Range}}
		<a href="{{$e.URL}}">View trace ({{$e.Name}})</a><br>
	{{end}}
	<br>
{{else}}
	<a href="/pprof/{{.Path}}/trace">View trace</a><br>
{{end}}
<a href="/pprof/{{.Path}}/goroutines">Goroutine analysis</a><br>
<a href="/pprof/{{.Path}}/io">Network blocking profile</a> (<a href="/io?raw=1" download="io.profile">⬇</a>)<br>
<a href="/pprof/{{.Path}}/block">Synchronization blocking profile</a> (<a href="/block?raw=1" download="block.profile">⬇</a>)<br>
<a href="/pprof/{{.Path}}/syscall">Syscall blocking profile</a> (<a href="/syscall?raw=1" download="syscall.profile">⬇</a>)<br>
<a href="/pprof/{{.Path}}/sched">Scheduler latency profile</a> (<a href="/sche?raw=1" download="sched.profile">⬇</a>)<br>
<a href="/pprof/{{.Path}}/usertasks">User-defined tasks</a><br>
<a href="/pprof/{{.Path}}/userregions">User-defined regions</a><br>
<a href="/pprof/{{.Path}}/mmu">Minimum mutator utilization</a><br>
</body>
</html>
`))
