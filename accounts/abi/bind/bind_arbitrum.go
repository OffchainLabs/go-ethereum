// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package bind generates Ethereum contract Go bindings.
//
// Detailed usage document and tutorial available on the go-ethereum Wiki page:
// https://github.com/ethereum/go-ethereum/wiki/Native-DApps:-Go-bindings-to-Ethereum-contracts

package bind

func Bind(types []string, abis []string, bytecodes []string, fsigs []map[string]string, pkg string, lang Lang, libs map[string]string, aliases map[string]string) (string, error) {
	return BindWithTemplate(types, abis, bytecodes, fsigs, pkg, lang, libs, aliases, tmplSource[lang])
}

const ArbTmplSourceGo = `
// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package {{.Package}}

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/vm"
)

var _ = abi.NewType

{{range $contract := .Contracts}}
	// {{.Type}}MetaData contains all meta data concerning the {{.Type}} contract.
	var {{.Type}}MetaData = &bind.MetaData{
		ABI: "{{.InputABI}}",
		{{if $contract.FuncSigs -}}
		Sigs: map[string]string{
			{{range $strsig, $binsig := .FuncSigs}}"{{$binsig}}": "{{$strsig}}",
			{{end}}
		},
		{{end -}}
		{{if .InputBin -}}
		Bin: "0x{{.InputBin}}",
		{{end}}
	}
{{end}}

{{$structs := .Structs}}
{{range $contract := .Contracts}}
	type {{$contract.Type}}Impl interface {
	// Non Mutating
	{{range .Calls}}
		{{.Normalized.Name}}(caller common.Address{{if ne .Normalized.StateMutability "pure"}}, st *state.StateDB{{end}}{{if .Normalized.Payable}}, value *big.Int,{{end}} {{range .Normalized.Inputs}}, {{.Name}} {{bindtype .Type $structs}} {{end}}) ({{if .Structured}}struct{ {{range .Normalized.Outputs}}{{.Name}} {{bindtype .Type $structs}};{{end}} },{{else}}{{range .Normalized.Outputs}}{{bindtype .Type $structs}},{{end}}{{end}} error)
		{{.Normalized.Name}}GasCost({{range .Normalized.Inputs}}{{.Name}} {{bindtype .Type $structs}}, {{end}}) uint64
	{{end}}

	// Mutating
	{{range .Transacts}}
		{{.Normalized.Name}}(caller common.Address{{if ne .Normalized.StateMutability "pure"}}, st *state.StateDB{{end}}{{if .Normalized.Payable}}, value *big.Int,{{end}} {{range .Normalized.Inputs}}, {{.Name}} {{bindtype .Type $structs}} {{end}}) ({{if .Structured}}struct{ {{range .Normalized.Outputs}}{{.Name}} {{bindtype .Type $structs}};{{end}} },{{else}}{{range .Normalized.Outputs}}{{bindtype .Type $structs}},{{end}}{{end}} error)
		{{.Normalized.Name}}GasCost({{range .Normalized.Inputs}}{{.Name}} {{bindtype .Type $structs}}, {{end}}) uint64
	{{end}}
	}

	type {{$contract.Type}} struct {
		impl {{$contract.Type}}Impl
	}

	func New{{$contract.Type}}(impl {{$contract.Type}}Impl) *{{$contract.Type}} {
		return &{{$contract.Type}}{impl: impl}
	}

	func (c *{{$contract.Type}}) GasToCharge(input []byte) uint64 {
		evmABI, err := {{.Type}}MetaData.GetAbi()
		if err != nil {
			return 0
		}
		method, err := evmABI.MethodById(input)
		if err != nil {
			return 0
		}
		args, err := method.Inputs.Unpack(input[4:])
		if err != nil {
			return 0
		}
		_ = args
		var id [4]byte
		copy(id[:], input)
		switch id { {{range .Calls}}
		case [4]byte{ {{range .Original.ID}} {{.}}, {{end}} }: {{range $i, $t := .Normalized.Inputs}} 
			{{.Name}} := *abi.ConvertType(args[{{$i}}], new({{bindtype .Type $structs}})).(*{{bindtype .Type $structs}}){{end}}
			return c.impl.{{.Normalized.Name}}GasCost({{range $i, $t := .Normalized.Inputs}}{{.Name}},{{end}}){{end}}{{range .Transacts}}
		case [4]byte{ {{range .Original.ID}} {{.}}, {{end}} }: {{range $i, $t := .Normalized.Inputs}} 
			{{.Name}} := *abi.ConvertType(args[{{$i}}], new({{bindtype .Type $structs}})).(*{{bindtype .Type $structs}}){{end}}
			return c.impl.{{.Normalized.Name}}GasCost({{range $i, $t := .Normalized.Inputs}}{{.Name}}, {{end}}){{end}}
		default:
			return 0
		}
	}

	func (c *{{$contract.Type}}) Call(
		input []byte,
		precompileAddress common.Address,
		actingAsAddress common.Address,
		caller common.Address,
		value *big.Int,
		readOnly bool,
		evm *vm.EVM,
	) ([]byte, error) {
		evmABI, err := {{.Type}}MetaData.GetAbi()
		if err != nil {
			return nil, err
		}
		args, outputs, err := checkCall(input, precompileAddress, actingAsAddress, value, readOnly, evmABI)
		if err != nil {
			return nil, err
		}
		stateDB, ok := evm.StateDB.(*state.StateDB)
		if !ok {
			panic("Expected statedb to be of type *state.StateDB")
		}
		_ = stateDB
		_ = args
		var id [4]byte
		copy(id[:], input)
		switch id { {{range .Calls}}
		case [4]byte{ {{range .Original.ID}} {{.}}, {{end}} }: {{range $i, $t := .Normalized.Inputs}} 
			{{.Name}} := *abi.ConvertType(args[{{$i}}], new({{bindtype .Type $structs}})).(*{{bindtype .Type $structs}}){{end}}
			{{range $i, $t := .Normalized.Outputs}}out{{$i}},{{end}}err := c.impl.{{.Normalized.Name}}(caller {{if ne .Normalized.StateMutability "pure"}}, stateDB{{end}}{{if .Normalized.Payable}}, value {{end}} {{range $i, $t := .Normalized.Inputs}}, {{.Name}}{{end}})
			if err != nil {
				return nil, err
			}
			return outputs.PackValues([]interface{}{ {{range $i, $t := .Normalized.Outputs}}out{{$i}},{{end}} }){{end}} {{range .Transacts}}
		case [4]byte{ {{range .Original.ID}} {{.}}, {{end}} }: {{range $i, $t := .Normalized.Inputs}} 
			{{.Name}} := *abi.ConvertType(args[{{$i}}], new({{bindtype .Type $structs}})).(*{{bindtype .Type $structs}}){{end}}
			{{range $i, $t := .Normalized.Outputs}}out{{$i}},{{end}}err := c.impl.{{.Normalized.Name}}(caller {{if ne .Normalized.StateMutability "pure"}}, stateDB{{end}}{{if .Normalized.Payable}}, value {{end}} {{range $i, $t := .Normalized.Inputs}}, {{.Name}}{{end}})
			if err != nil {
				return nil, err
			}
			return outputs.PackValues([]interface{}{ {{range $i, $t := .Normalized.Outputs}}out{{$i}},{{end}} }){{end}}
		default:
			return nil, errors.New("unsupported method")
		}
	}
{{end}}
`