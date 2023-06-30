package util

import "fmt"

type InstructionNotFound struct {
	Opcode int64
}

func (e *InstructionNotFound) Error() string {
	return fmt.Sprintf("Could not find instruction for opcode %d", e.Opcode)
}
