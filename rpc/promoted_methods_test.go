package rpc

import (
	"strings"
	"testing"
)

func newEmbedTestServer(t *testing.T) *Server {
	server := NewServer()
	testService := &embeddingTestService{
		embeddedTestStructBCD:              &embeddedTestStructBCD{},
		embeddedTestStructEFG:              embeddedTestStructEFG{},
		recursivelyEmbeddedTestStruct:      &recursivelyEmbeddedTestStruct{&embeddedTestStructHIJ{}},
		valueRecursivelyEmbeddedTestStruct: valueRecursivelyEmbeddedTestStruct{embeddedTestStructKLM{}},
	}
	if err := server.RegisterName("test", testService); err != nil {
		t.Fatalf("internal test error, RegisterName failed: %v", err)
	}
	return server
}

type embeddedTestStructBCD struct {
}

func (*embeddedTestStructBCD) B() {}
func (embeddedTestStructBCD) C()  {}
func (*embeddedTestStructBCD) D() {}

type embeddedTestStructEFG struct {
}

func (*embeddedTestStructEFG) E() {}
func (embeddedTestStructEFG) F()  {}
func (*embeddedTestStructEFG) G() {}

type embeddedTestStructHIJ struct {
}

func (*embeddedTestStructHIJ) H() {}
func (embeddedTestStructHIJ) I()  {}
func (embeddedTestStructHIJ) J()  {}

type recursivelyEmbeddedTestStruct struct {
	*embeddedTestStructHIJ
}

type embeddedTestStructKLM struct {
}

func (*embeddedTestStructKLM) K() {}
func (embeddedTestStructKLM) L()  {}
func (*embeddedTestStructKLM) M() {}

type valueRecursivelyEmbeddedTestStruct struct {
	embeddedTestStructKLM
}

type embeddingTestService struct {
	*embeddedTestStructBCD
	embeddedTestStructEFG
	*recursivelyEmbeddedTestStruct
	valueRecursivelyEmbeddedTestStruct
}

func (*embeddingTestService) A() {}

func (s *embeddingTestService) D() {
	s.embeddedTestStructBCD.D()
}
func (s *embeddingTestService) G() {
	s.embeddedTestStructEFG.G()
}
func (s *embeddingTestService) J() {
	s.recursivelyEmbeddedTestStruct.J()
}
func (s *embeddingTestService) M() {
	s.valueRecursivelyEmbeddedTestStruct.M()
}

func TestPromotedMethodsNotExposed(t *testing.T) {
	t.Parallel()

	server := newEmbedTestServer(t)
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	if err := client.Call(nil, "test_a"); err != nil {
		t.Errorf("test_a should be reachable, but got an error: %v", err)
	}

	for _, method := range []string{"test_b", "test_c", "test_d", "test_e", "test_f", "test_g", "test_h", "test_i", "test_j", "test_k", "test_l", "test_m"} {
		if err := client.Call(nil, method); err == nil {
			t.Errorf("%s should not be reachable, but didn't get an error", method)
		} else if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("%s should not be reachable, but got an unexpected error: %v", method, err)
		}
	}
}

func TestPromotedMethodsPointerToPointerRegistration(t *testing.T) {
	t.Parallel()

	server := NewServer()
	expectedErrorMsg := "doesn't have any suitable methods/subscriptions to expose"

	pointer := &embeddedTestStructBCD{}
	pointerToPointer := &pointer
	if err := server.RegisterName("test", pointerToPointer); err == nil {
		t.Error("RegisterName should have failed")
	} else if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("RegisterName failed with unexpected error: %v", err)
	}
}

func TestPromotedMethodsEmbedOnly(t *testing.T) {
	t.Parallel()

	server := NewServer()
	expectedErrorMsg := "doesn't have any suitable methods/subscriptions to expose"

	embedOnly := &struct{ *embeddedTestStructBCD }{
		&embeddedTestStructBCD{},
	}
	if err := server.RegisterName("test", embedOnly); err == nil {
		t.Error("RegisterName should have failed")
	} else if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("RegisterName failed with unexpected error: %v", err)
	}
}
