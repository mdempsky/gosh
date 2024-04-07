// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The gosh command runs shell commands embedded in Go source file comments.
//
// Usage:
//
//	gosh [-w] [packages]
//
// Gosh searches source files for comments that start with "// % " or "/* % ".
// It then runs the first line of the comment as a shell command,
// and replaces the remaining lines with the output of the command.
// It also replaces the "%" with "#".
// Shell commands are run concurrently.
//
// For security, shell commands are disabled by default.
// The "//gosh:ok" directive enables commands,
// and the "//gosh:deny" directive disables them again.
// Both directives only apply to the end of their innermost scope.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"go/scanner"
	"go/token"
	"log"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/go/packages"
)

var flagWrite = flag.Bool("w", false, "write result back to source file instead of stdout")

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		args = []string{"."}
	}

	cfg := packages.Config{
		Mode: packages.NeedFiles,
	}
	pkgs, err := packages.Load(&cfg, args...)
	if err != nil {
		log.Fatal(err)
	}

	var g errgroup.Group
	for _, pkg := range pkgs {
		for _, filePath := range pkg.GoFiles {
			g.Go(func() error {
				return gosh(filePath)
			})
		}
	}
	if err := g.Wait(); err != nil {
		log.Fatal(err)
	}
}

func gosh(filePath string) error {
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()
	file := fset.AddFile(filePath, -1, len(fileData))

	var s scanner.Scanner
	s.Init(file, fileData, nil, scanner.ScanComments)

	allowed := stack[bool]{false}

	type edit struct {
		pos, end token.Pos
		text     string
	}
	var asyncEdits asyncSlice[edit]
Outer:
	for {
		switch pos, tok, lit := s.Scan(); tok {
		case token.EOF:
			break Outer

		case token.LBRACE:
			allowed.push(allowed.top())

		case token.RBRACE:
			allowed.pop()

		case token.COMMENT:
			// Process directives.
			const prefix = "//gosh:"
			if cmd, ok := strings.CutPrefix(lit, prefix); ok {
				pos := pos + token.Pos(len(prefix))
				switch cmd {
				case "ok":
					fmt.Printf("%s: ok\n", fset.Position(pos))
					allowed.setTop(true)
				case "deny":
					fmt.Printf("%s: deny\n", fset.Position(pos))
					allowed.setTop(false)
				default:
					log.Fatalf("%s: unknown command: %s\n", fset.Position(pos), cmd)
				}
				continue
			}

			// Unit testing logic.
			if false {
				want := func(ok bool) {
					if allowed.top() != ok {
						log.Fatalf("%s: want ok=%v, but allowed=%v", fset.Position(pos), ok, allowed)
					}
				}
				switch text := lit; {
				case strings.Contains(text, "% ok"):
					want(true)
				case strings.Contains(text, "% FAIL"):
					want(false)
				}
			}

			if !allowed.top() {
				continue
			}

			prompt, ok := strings.CutPrefix(lit[2:], " % ")
			if !ok {
				continue
			}
			prompt, _, _ = strings.Cut(prompt, "\n")
			prompt = strings.TrimSpace(prompt)

			asyncEdits.append(func() (edit, error) {
				cmd := exec.Command("sh", "-c", prompt)
				output, err := cmd.Output()
				if err != nil {
					return edit{}, fmt.Errorf("%s: %v", fset.Position(pos), err)
				}
				text := fmt.Sprintf("/* # %s\n%s*/", prompt, output)
				return edit{pos, pos + token.Pos(len(lit)), text}, nil
			})
		}
	}

	edits, err := asyncEdits.wait()
	if err != nil {
		return err
	}

	base := token.Pos(file.Base())
	var buf bytes.Buffer
	pos := base
	for _, edit := range edits {
		buf.Write(fileData[pos-base : edit.pos-base])
		buf.WriteString(edit.text)
		pos = edit.end
	}
	buf.Write(fileData[pos-base:])

	out, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}

	if *flagWrite {
		return os.WriteFile(filePath, out, 0666)
	}

	fmt.Printf("-- %s --\n%s", filePath, out)
	return nil
}

func _testdata() {
	// By default, shell commands should not run.
	// This is necessary for security.
	//
	// % FAIL

	{
		// Even within a block where commands are later allowed,
		// we don't allow them before the directive.
		// This is simpler to implement, and also encourages
		// keeping the directives near the top.
		//
		// % FAIL

		//gosh:ok

		// Within a block marked with "gosh:ok",
		// shell commands are allowed.
		//
		// % echo ok

		{
			// This includes nested blocks too.
			//
			// % echo ok
		}

		// And multiline comments.
		//
		/* % echo ok
		really, it's fine
		  foo
		bar
		*/
	}

	// But back to the outer scope,
	// it should be denied again.
	//
	// % FAIL
}

type stack[T any] []T

func (s *stack[T]) push(t T)  { *s = append(*s, t) }
func (s *stack[T]) pop()      { *s = (*s)[:len(*s)-1] }
func (s stack[T]) top() T     { return s[len(s)-1] }
func (s stack[T]) setTop(t T) { s[len(s)-1] = t }

type asyncSlice[T any] struct {
	g errgroup.Group
	s []T
}

func (s *asyncSlice[T]) append(fn func() (T, error)) {
	i := len(s.s)
	s.s = append(s.s, *new(T))
	s.g.Go(func() error {
		var err error
		s.s[i], err = fn()
		return err
	})
}

func (s *asyncSlice[T]) wait() ([]T, error) {
	return s.s, s.g.Wait()
}
