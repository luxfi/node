package main
import (
  "fmt"
  "github.com/luxfi/node/ids"
)
func main() {
  id := ids.ID{'m','v','m'}
  fmt.Println(id.String())
}
