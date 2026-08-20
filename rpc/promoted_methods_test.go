package rpc

import (
	"strings"
	"testing"
)

func newEmbedTestServer() *Server {
	server := NewServer()
	testService := &embeddingTestService{
		embeddedTestStructBC: new(embeddedTestStructBC),
		embeddedTestStructDE: embeddedTestStructDE{},
	}
	if err := server.RegisterName("test", testService); err != nil {
		panic(err)
	}
	return server
}

type embeddedTestStructBC struct {
}

func (*embeddedTestStructBC) B() {}
func (*embeddedTestStructBC) C() {}

type embeddedTestStructDE struct {
}

func (*embeddedTestStructDE) D() {}
func (*embeddedTestStructDE) E() {}

type embeddingTestService struct {
	*embeddedTestStructBC
	embeddedTestStructDE
}

func (*embeddingTestService) A() {}
func (s *embeddingTestService) B() {
	s.embeddedTestStructBC.B()
}
func (s *embeddingTestService) D() {
	s.embeddedTestStructDE.D()
}

func TestPromotedMethodsNotExposed(t *testing.T) {
	t.Parallel()

	server := newEmbedTestServer()
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	if err := client.Call(nil, "test_a"); err != nil {
		t.Errorf("test_a should be reachable, but got an error: %v", err)
	}

	for _, method := range []string{"test_b", "test_c", "test_d", "test_e"} {
		if err := client.Call(nil, method); err == nil {
			t.Errorf("%s should not be reachable, but didn't get an error", method)
		} else if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("%s should not be reachable, but got an unexpected error: %v", method, err)
		}
	}
}
