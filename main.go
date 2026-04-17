package main

import (
	"fmt"
	"time"
)

func main() {
	now := time.Now().Format(time.DateTime)
	fmt.Printf("Something so good, current time is %s\n", now)
}
