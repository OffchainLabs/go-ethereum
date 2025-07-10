// Package multigas defines multi-dimensional gas for the EVM.
//
// This package introduces mechanisms to track each resource used by the EVM separately. The
// possible resources are computation, history growth, storage access, and storage growth. By
// tracking each one individually and setting specific constraints, we can increase the overall gas
// target for the chain.
package multigas
