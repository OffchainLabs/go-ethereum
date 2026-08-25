// This test calls a method that returns a typed-nil pointer implementing
// json.Marshaler. The server must encode it as null instead of crashing.

--> {"jsonrpc":"2.0","id":1,"method":"test_nilMarshaler","params":[]}
<-- {"jsonrpc":"2.0","id":1,"result":null}
