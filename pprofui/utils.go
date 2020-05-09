package pprofui

import (
	"html/template"
	"net/http"
)

func traceUIhttpMain(w http.ResponseWriter, r *http.Request, ranges []Range) {
	if err := templMain.Execute(w, ranges); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

var templMain = template.Must(template.New("").Parse(`
<html>
<body>
{{if $}}
	{{range $e := $}}
		<a href="{{$e.URL}}">View trace ({{$e.Name}})</a><br>
	{{end}}
	<br>
{{else}}
	<a href="/trace">View trace</a><br>
{{end}}
<a href="/goroutines">Goroutine analysis</a><br>
<a href="/io">Network blocking profile</a> (<a href="/io?raw=1" download="io.profile">⬇</a>)<br>
<a href="/block">Synchronization blocking profile</a> (<a href="/block?raw=1" download="block.profile">⬇</a>)<br>
<a href="/syscall">Syscall blocking profile</a> (<a href="/syscall?raw=1" download="syscall.profile">⬇</a>)<br>
<a href="/sched">Scheduler latency profile</a> (<a href="/sche?raw=1" download="sched.profile">⬇</a>)<br>
<a href="/usertasks">User-defined tasks</a><br>
<a href="/userregions">User-defined regions</a><br>
<a href="/mmu">Minimum mutator utilization</a><br>
</body>
</html>
`))
