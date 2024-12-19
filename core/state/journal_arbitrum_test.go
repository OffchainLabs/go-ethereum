package state

import "testing"

func TestIsZombie(t *testing.T) {
	var nonZombie journalEntry = createObjectChange{}
	if isZombie(nonZombie) {
		t.Error("createObjectChange should not be a zombie")
	}
	var zombie journalEntry = createZombieChange{}
	if !isZombie(zombie) {
		t.Error("createZombieChange should be a zombie")
	}
}
