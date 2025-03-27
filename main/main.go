package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/sejust/slasher/code/unique"
)

func main() {
	defer func() {
		if p := recover(); p != nil {
			fmt.Println(p)
			fmt.Println(string(debug.Stack()))
			time.Sleep(time.Hour)
		}
	}()
	if len(os.Args) > 1 {
		unique.Decompress(os.Args[1])
		return
	}
	unique.Run()
}
