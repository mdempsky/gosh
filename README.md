gosh: run shell commands embedded in Go comments

Install: `go install github.com/mdempsky/gosh@latest`

Demo:

```
$ cat demo.go
package demo

//go:generate gosh -w $GOFILE
//gosh:ok

// % date
$ go generate demo.go
$ cat demo.go
package demo

//go:generate gosh -w $GOFILE
//gosh:ok

/* # date
Mon Apr  8 12:22:29 PM PDT 2024
*/
```


% cat demo.go
package demo

//gosh:ok

// % date
```

```
% gosh demo.go
-- /home/mdempsky/wd/demo.go --
package demo

//gosh:ok

/* # date
Mon Apr  8 12:16:39 PM PDT 2024
*/
```
