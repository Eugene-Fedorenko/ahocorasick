package cedar

import (
	"testing"
)

func TestFindOne(t *testing.T) {
	m := NewCedar()

	words := []string{
		"aaa*bbb*ccc",
		"*",
	}

	for i, word := range words {
		_ = m.Insert([]byte(word), i+100)
	}

	seq := []byte("aaaabbbbcccc")

	k, e := m.FindOne(seq)

	if e != nil {
		t.Error(e)
	}

	ik, ok := k.(int)
	if !ok {
		t.Fail()
	}

	if ik != 100 {
		t.Fail()
	}
}
