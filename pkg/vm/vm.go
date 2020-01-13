package vm

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"reflect"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/infinitete/neo-go/pkg/crypto/hash"
	"github.com/infinitete/neo-go/pkg/crypto/keys"
	"github.com/infinitete/neo-go/pkg/util"
	"github.com/pkg/errors"
)

type errorAtInstruct struct {
	ip  int
	op  Instruction
	err interface{}
}

func (e *errorAtInstruct) Error() string {
	return fmt.Sprintf("error encountered at instruction %d (%s): %s", e.ip, e.op, e.err)
}

func newError(ip int, op Instruction, err interface{}) *errorAtInstruct {
	return &errorAtInstruct{ip: ip, op: op, err: err}
}

// StateMessage is a vm state message which could be used as additional info for example by cli.
type StateMessage string

const (
	// MaxArraySize is the maximum array size allowed in the VM.
	MaxArraySize = 1024

	// MaxItemSize is the maximum item size allowed in the VM.
	MaxItemSize = 1024 * 1024

	// MaxInvocationStackSize is the maximum size of an invocation stack.
	MaxInvocationStackSize = 1024

	// MaxBigIntegerSizeBits is the maximum size of BigInt item in bits.
	MaxBigIntegerSizeBits = 32 * 8

	// MaxStackSize is the maximum number of items allowed to be
	// on all stacks at once.
	MaxStackSize = 2 * 1024

	maxSHLArg = MaxBigIntegerSizeBits
	minSHLArg = -MaxBigIntegerSizeBits
)

// VM represents the virtual machine.
type VM struct {
	state State

	// registered interop hooks.
	interop map[string]InteropFuncPrice

	// callback to get scripts.
	getScript func(util.Uint160) []byte

	istack *Stack // invocation stack.
	estack *Stack // execution stack.
	astack *Stack // alt stack.

	// Hash to verify in CHECKSIG/CHECKMULTISIG.
	checkhash []byte

	itemCount map[StackItem]int
	size      int
}

// InteropFuncPrice represents an interop function with a price.
type InteropFuncPrice struct {
	Func  InteropFunc
	Price int
}

// New returns a new VM object ready to load .avm bytecode scripts.
func New() *VM {
	vm := &VM{
		interop:   make(map[string]InteropFuncPrice),
		getScript: nil,
		state:     haltState,
		istack:    NewStack("invocation"),

		itemCount: make(map[StackItem]int),
	}

	vm.estack = vm.newItemStack("evaluation")
	vm.astack = vm.newItemStack("alt")

	// Register native interop hooks.
	vm.RegisterInteropFunc("Neo.Runtime.Log", runtimeLog, 1)
	vm.RegisterInteropFunc("Neo.Runtime.Notify", runtimeNotify, 1)
	vm.RegisterInteropFunc("Neo.Runtime.Serialize", RuntimeSerialize, 1)
	vm.RegisterInteropFunc("System.Runtime.Serialize", RuntimeSerialize, 1)
	vm.RegisterInteropFunc("Neo.Runtime.Deserialize", RuntimeDeserialize, 1)
	vm.RegisterInteropFunc("System.Runtime.Deserialize", RuntimeDeserialize, 1)

	return vm
}

func (v *VM) newItemStack(n string) *Stack {
	s := NewStack(n)
	s.size = &v.size
	s.itemCount = v.itemCount

	return s
}

// RegisterInteropFunc registers the given InteropFunc to the VM.
func (v *VM) RegisterInteropFunc(name string, f InteropFunc, price int) {
	v.interop[name] = InteropFuncPrice{f, price}
}

// RegisterInteropFuncs registers all interop functions passed in a map in
// the VM. Effectively it's a batched version of RegisterInteropFunc.
func (v *VM) RegisterInteropFuncs(interops map[string]InteropFuncPrice) {
	// We allow reregistration here.
	for name, funPrice := range interops {
		v.interop[name] = funPrice
	}
}

// Estack returns the evaluation stack so interop hooks can utilize this.
func (v *VM) Estack() *Stack {
	return v.estack
}

// Astack returns the alt stack so interop hooks can utilize this.
func (v *VM) Astack() *Stack {
	return v.astack
}

// Istack returns the invocation stack so interop hooks can utilize this.
func (v *VM) Istack() *Stack {
	return v.istack
}

// LoadArgs loads in the arguments used in the Mian entry point.
func (v *VM) LoadArgs(method []byte, args []StackItem) {
	if len(args) > 0 {
		v.estack.PushVal(args)
	}
	if method != nil {
		v.estack.PushVal(method)
	}
}

// PrintOps prints the opcodes of the current loaded program to stdout.
func (v *VM) PrintOps() {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "INDEX\tOPCODE\tPARAMETER\t")
	realctx := v.Context()
	ctx := realctx.Copy()
	ctx.ip = 0
	ctx.nextip = 0
	for {
		cursor := ""
		instr, parameter, err := ctx.Next()
		if ctx.ip == realctx.ip {
			cursor = "<<"
		}
		if err != nil {
			fmt.Fprintf(w, "%d\t%s\tERROR: %s\t%s\n", ctx.ip, instr, err, cursor)
			break
		}
		var desc = ""
		if parameter != nil {
			switch instr {
			case JMP, JMPIF, JMPIFNOT, CALL:
				offset := int16(binary.LittleEndian.Uint16(parameter))
				desc = fmt.Sprintf("%d (%d/%x)", ctx.ip+int(offset), offset, parameter)
			case SYSCALL:
				desc = fmt.Sprintf("%q", parameter)
			case APPCALL, TAILCALL:
				desc = fmt.Sprintf("%x", parameter)
			default:
				if utf8.Valid(parameter) {
					desc = fmt.Sprintf("%x (%q)", parameter, parameter)
				} else {
					desc = fmt.Sprintf("%x", parameter)
				}
			}
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", ctx.ip, instr, desc, cursor)
		if ctx.nextip >= len(ctx.prog) {
			break
		}
	}
	w.Flush()
}

// AddBreakPoint adds a breakpoint to the current context.
func (v *VM) AddBreakPoint(n int) {
	ctx := v.Context()
	ctx.breakPoints = append(ctx.breakPoints, n)
}

// AddBreakPointRel adds a breakpoint relative to the current
// instruction pointer.
func (v *VM) AddBreakPointRel(n int) {
	ctx := v.Context()
	v.AddBreakPoint(ctx.ip + n)
}

// LoadFile loads a program from the given path, ready to execute it.
func (v *VM) LoadFile(path string) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	v.Load(b)
	return nil
}

// Load initializes the VM with the program given.
func (v *VM) Load(prog []byte) {
	// Clear all stacks and state, it could be a reload.
	v.istack.Clear()
	v.estack.Clear()
	v.astack.Clear()
	v.state = noneState
	v.LoadScript(prog)
}

// LoadScript loads a script from the internal script table. It
// will immediately push a new context created from this script to
// the invocation stack and starts executing it.
func (v *VM) LoadScript(b []byte) {
	ctx := NewContext(b)
	ctx.estack = v.estack
	ctx.astack = v.astack
	v.istack.PushVal(ctx)
}

// Context returns the current executed context. Nil if there is no context,
// which implies no program is loaded.
func (v *VM) Context() *Context {
	if v.istack.Len() == 0 {
		return nil
	}
	return v.istack.Peek(0).Value().(*Context)
}

// PopResult is used to pop the first item of the evaluation stack. This allows
// us to test compiler and vm in a bi-directional way.
func (v *VM) PopResult() interface{} {
	e := v.estack.Pop()
	if e != nil {
		return e.Value()
	}
	return nil
}

// Stack returns json formatted representation of the given stack.
func (v *VM) Stack(n string) string {
	var s *Stack
	if n == "astack" {
		s = v.astack
	}
	if n == "istack" {
		s = v.istack
	}
	if n == "estack" {
		s = v.estack
	}
	return buildStackOutput(s)
}

// State returns string representation of the state for the VM.
func (v *VM) State() string {
	return v.state.String()
}

// Ready returns true if the VM ready to execute the loaded program.
// Will return false if no program is loaded.
func (v *VM) Ready() bool {
	return v.istack.Len() > 0
}

// Run starts the execution of the loaded program.
func (v *VM) Run() error {
	if !v.Ready() {
		v.state = faultState
		return errors.New("no program loaded")
	}

	if v.state.HasFlag(faultState) {
		// VM already ran something and failed, in general its state is
		// undefined in this case so we can't run anything.
		return errors.New("VM has failed")
	}
	// haltState (the default) or breakState are safe to continue.
	v.state = noneState
	for {
		// check for breakpoint before executing the next instruction
		ctx := v.Context()
		if ctx != nil && ctx.atBreakPoint() {
			v.state |= breakState
		}
		switch {
		case v.state.HasFlag(faultState):
			// Should be caught and reported already by the v.Step(),
			// but we're checking here anyway just in case.
			return errors.New("VM has failed")
		case v.state.HasFlag(haltState), v.state.HasFlag(breakState):
			// Normal exit from this loop.
			return nil
		case v.state == noneState:
			if err := v.Step(); err != nil {
				return err
			}
		default:
			v.state = faultState
			return errors.New("unknown state")
		}
	}
}

// Step 1 instruction in the program.
func (v *VM) Step() error {
	ctx := v.Context()
	op, param, err := ctx.Next()
	if err != nil {
		v.state = faultState
		return newError(ctx.ip, op, err)
	}
	return v.execute(ctx, op, param)
}

// StepInto behaves the same as “step over” in case if the line does not contain a function. Otherwise
// the debugger will enter the called function and continue line-by-line debugging there.
func (v *VM) StepInto() error {
	ctx := v.Context()

	if ctx == nil {
		v.state |= haltState
	}

	if v.HasStopped() {
		return nil
	}

	if ctx != nil && ctx.prog != nil {
		op, param, err := ctx.Next()
		if err != nil {
			v.state = faultState
			return newError(ctx.ip, op, err)
		}
		vErr := v.execute(ctx, op, param)
		if vErr != nil {
			return vErr
		}
	}

	cctx := v.Context()
	if cctx != nil && cctx.atBreakPoint() {
		v.state = breakState
	}
	return nil
}

// StepOut takes the debugger to the line where the current function was called.
func (v *VM) StepOut() error {
	var err error
	if v.state == breakState {
		v.state = noneState
	} else {
		v.state = breakState
	}

	expSize := v.istack.len
	for v.state == noneState && v.istack.len >= expSize {
		err = v.StepInto()
	}
	return err
}

// StepOver takes the debugger to the line that will step over a given line.
// If the line contains a function the function will be executed and the result returned without debugging each line.
func (v *VM) StepOver() error {
	var err error
	if v.HasStopped() {
		return err
	}

	if v.state == breakState {
		v.state = noneState
	} else {
		v.state = breakState
	}

	expSize := v.istack.len
	for {
		err = v.StepInto()
		if !(v.state == noneState && v.istack.len > expSize) {
			break
		}
	}

	if v.state == noneState {
		v.state = breakState
	}

	return err
}

// HasFailed returns whether VM is in the failed state now. Usually used to
// check status after Run.
func (v *VM) HasFailed() bool {
	return v.state.HasFlag(faultState)
}

// HasStopped returns whether VM is in Halt or Failed state.
func (v *VM) HasStopped() bool {
	return v.state.HasFlag(haltState) || v.state.HasFlag(faultState)
}

// HasHalted returns whether VM is in Halt state.
func (v *VM) HasHalted() bool {
	return v.state.HasFlag(haltState)
}

// AtBreakpoint returns whether VM is at breakpoint.
func (v *VM) AtBreakpoint() bool {
	return v.state.HasFlag(breakState)
}

// SetCheckedHash sets checked hash for CHECKSIG and CHECKMULTISIG instructions.
func (v *VM) SetCheckedHash(h []byte) {
	v.checkhash = make([]byte, len(h))
	copy(v.checkhash, h)
}

// SetScriptGetter sets the script getter for CALL instructions.
func (v *VM) SetScriptGetter(gs func(util.Uint160) []byte) {
	v.getScript = gs
}

// execute performs an instruction cycle in the VM. Acting on the instruction (opcode).
func (v *VM) execute(ctx *Context, op Instruction, parameter []byte) (err error) {
	// Instead of polluting the whole VM logic with error handling, we will recover
	// each panic at a central point, putting the VM in a fault state and setting error.
	defer func() {
		if errRecover := recover(); errRecover != nil {
			v.state = faultState
			err = newError(ctx.ip, op, errRecover)
		} else if v.size > MaxStackSize {
			v.state = faultState
			err = newError(ctx.ip, op, "stack is too big")
		}
	}()

	if op >= PUSHBYTES1 && op <= PUSHBYTES75 {
		v.estack.PushVal(parameter)
		return
	}

	switch op {
	case PUSHM1, PUSH1, PUSH2, PUSH3, PUSH4, PUSH5,
		PUSH6, PUSH7, PUSH8, PUSH9, PUSH10, PUSH11,
		PUSH12, PUSH13, PUSH14, PUSH15, PUSH16:
		val := int(op) - int(PUSH1) + 1
		v.estack.PushVal(val)

	case PUSH0:
		v.estack.PushVal([]byte{})

	case PUSHDATA1, PUSHDATA2, PUSHDATA4:
		v.estack.PushVal(parameter)

	// Stack operations.
	case TOALTSTACK:
		v.astack.Push(v.estack.Pop())

	case FROMALTSTACK:
		v.estack.Push(v.astack.Pop())

	case DUPFROMALTSTACK:
		v.estack.Push(v.astack.Dup(0))

	case DUP:
		v.estack.Push(v.estack.Dup(0))

	case SWAP:
		a := v.estack.Pop()
		b := v.estack.Pop()
		v.estack.Push(a)
		v.estack.Push(b)

	case TUCK:
		a := v.estack.Dup(0)
		if a == nil {
			panic("no top-level element found")
		}
		if v.estack.Len() < 2 {
			panic("can't TUCK with a one-element stack")
		}
		v.estack.InsertAt(a, 2)

	case CAT:
		b := v.estack.Pop().Bytes()
		a := v.estack.Pop().Bytes()
		if l := len(a) + len(b); l > MaxItemSize {
			panic(fmt.Sprintf("too big item: %d", l))
		}
		ab := append(a, b...)
		v.estack.PushVal(ab)

	case SUBSTR:
		l := int(v.estack.Pop().BigInt().Int64())
		if l < 0 {
			panic("negative length")
		}
		o := int(v.estack.Pop().BigInt().Int64())
		if o < 0 {
			panic("negative index")
		}
		s := v.estack.Pop().Bytes()
		if o > len(s) {
			// panic("invalid offset")
			// FIXME revert when NEO 3.0 https://github.com/nspcc-dev/neo-go/issues/477
			v.estack.PushVal("")
			break
		}
		last := l + o
		if last > len(s) {
			last = len(s)
		}
		v.estack.PushVal(s[o:last])

	case LEFT:
		l := int(v.estack.Pop().BigInt().Int64())
		if l < 0 {
			panic("negative length")
		}
		s := v.estack.Pop().Bytes()
		if t := len(s); l > t {
			l = t
		}
		v.estack.PushVal(s[:l])

	case RIGHT:
		l := int(v.estack.Pop().BigInt().Int64())
		if l < 0 {
			panic("negative length")
		}
		s := v.estack.Pop().Bytes()
		v.estack.PushVal(s[len(s)-l:])

	case XDROP:
		n := int(v.estack.Pop().BigInt().Int64())
		if n < 0 {
			panic("invalid length")
		}
		e := v.estack.RemoveAt(n)
		if e == nil {
			panic("bad index")
		}

	case XSWAP:
		n := int(v.estack.Pop().BigInt().Int64())
		if n < 0 {
			panic("XSWAP: invalid length")
		}

		// Swap values of elements instead of reordering stack elements.
		if n > 0 {
			a := v.estack.Peek(n)
			b := v.estack.Peek(0)
			aval := a.value
			bval := b.value
			a.value = bval
			b.value = aval
		}

	case XTUCK:
		n := int(v.estack.Pop().BigInt().Int64())
		if n <= 0 {
			panic("XTUCK: invalid length")
		}
		a := v.estack.Dup(0)
		if a == nil {
			panic("no top-level element found")
		}
		if n > v.estack.Len() {
			panic("can't push to the position specified")
		}
		v.estack.InsertAt(a, n)

	case ROT:
		e := v.estack.RemoveAt(2)
		if e == nil {
			panic("no top-level element found")
		}
		v.estack.Push(e)

	case DEPTH:
		v.estack.PushVal(v.estack.Len())

	case NIP:
		elem := v.estack.RemoveAt(1)
		if elem == nil {
			panic("no second element found")
		}

	case OVER:
		a := v.estack.Peek(1)
		if a == nil {
			panic("no second element found")
		}
		v.estack.Push(a)

	case PICK:
		n := int(v.estack.Pop().BigInt().Int64())
		if n < 0 {
			panic("negative stack item returned")
		}
		a := v.estack.Peek(n)
		if a == nil {
			panic("no nth element found")
		}
		v.estack.Push(a)

	case ROLL:
		n := int(v.estack.Pop().BigInt().Int64())
		if n < 0 {
			panic("negative stack item returned")
		}
		if n > 0 {
			e := v.estack.RemoveAt(n)
			if e == nil {
				panic("bad index")
			}
			v.estack.Push(e)
		}

	case DROP:
		if v.estack.Len() < 1 {
			panic("stack is too small")
		}
		v.estack.Pop()

	case EQUAL:
		b := v.estack.Pop()
		if b == nil {
			panic("no top-level element found")
		}
		a := v.estack.Pop()
		if a == nil {
			panic("no second-to-the-top element found")
		}
		if ta, ok := a.value.(*ArrayItem); ok {
			if tb, ok := b.value.(*ArrayItem); ok {
				v.estack.PushVal(ta == tb)
				break
			}
		} else if ma, ok := a.value.(*MapItem); ok {
			if mb, ok := b.value.(*MapItem); ok {
				v.estack.PushVal(ma == mb)
				break
			}
		}
		v.estack.PushVal(reflect.DeepEqual(a, b))

	// Bit operations.
	case INVERT:
		// inplace
		a := v.estack.Peek(0).BigInt()
		a.Not(a)

	case AND:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(new(big.Int).And(b, a))

	case OR:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(new(big.Int).Or(b, a))

	case XOR:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(new(big.Int).Xor(b, a))

	// Numeric operations.
	case ADD:
		a := v.estack.Pop().BigInt()
		v.checkBigIntSize(a)
		b := v.estack.Pop().BigInt()
		v.checkBigIntSize(b)

		c := new(big.Int).Add(a, b)
		v.checkBigIntSize(c)
		v.estack.PushVal(c)

	case SUB:
		b := v.estack.Pop().BigInt()
		v.checkBigIntSize(b)
		a := v.estack.Pop().BigInt()
		v.checkBigIntSize(a)

		c := new(big.Int).Sub(a, b)
		v.checkBigIntSize(c)
		v.estack.PushVal(c)

	case DIV:
		b := v.estack.Pop().BigInt()
		v.checkBigIntSize(b)
		a := v.estack.Pop().BigInt()
		v.checkBigIntSize(a)

		v.estack.PushVal(new(big.Int).Div(a, b))

	case MUL:
		a := v.estack.Pop().BigInt()
		v.checkBigIntSize(a)
		b := v.estack.Pop().BigInt()
		v.checkBigIntSize(b)

		c := new(big.Int).Mul(a, b)
		v.checkBigIntSize(c)
		v.estack.PushVal(c)

	case MOD:
		b := v.estack.Pop().BigInt()
		v.checkBigIntSize(b)
		a := v.estack.Pop().BigInt()
		v.checkBigIntSize(a)

		v.estack.PushVal(new(big.Int).Mod(a, b))

	case SHL, SHR:
		b := v.estack.Pop().BigInt().Int64()
		if b == 0 {
			return
		} else if b < minSHLArg || b > maxSHLArg {
			panic(fmt.Sprintf("operand must be between %d and %d", minSHLArg, maxSHLArg))
		}
		a := v.estack.Pop().BigInt()
		v.checkBigIntSize(a)

		var item big.Int
		if op == SHL {
			item.Lsh(a, uint(b))
		} else {
			item.Rsh(a, uint(b))
		}

		v.checkBigIntSize(&item)
		v.estack.PushVal(&item)

	case BOOLAND:
		b := v.estack.Pop().Bool()
		a := v.estack.Pop().Bool()
		v.estack.PushVal(a && b)

	case BOOLOR:
		b := v.estack.Pop().Bool()
		a := v.estack.Pop().Bool()
		v.estack.PushVal(a || b)

	case NUMEQUAL:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(a.Cmp(b) == 0)

	case NUMNOTEQUAL:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(a.Cmp(b) != 0)

	case LT:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(a.Cmp(b) == -1)

	case GT:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(a.Cmp(b) == 1)

	case LTE:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(a.Cmp(b) <= 0)

	case GTE:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushVal(a.Cmp(b) >= 0)

	case MIN:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		val := a
		if a.Cmp(b) == 1 {
			val = b
		}
		v.estack.PushVal(val)

	case MAX:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		val := a
		if a.Cmp(b) == -1 {
			val = b
		}
		v.estack.PushVal(val)

	case WITHIN:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		x := v.estack.Pop().BigInt()
		v.estack.PushVal(a.Cmp(x) <= 0 && x.Cmp(b) == -1)

	case INC:
		x := v.estack.Pop().BigInt()
		a := new(big.Int).Add(x, big.NewInt(1))
		v.checkBigIntSize(a)
		v.estack.PushVal(a)

	case DEC:
		x := v.estack.Pop().BigInt()
		a := new(big.Int).Sub(x, big.NewInt(1))
		v.checkBigIntSize(a)
		v.estack.PushVal(a)

	case SIGN:
		x := v.estack.Pop().BigInt()
		v.estack.PushVal(x.Sign())

	case NEGATE:
		x := v.estack.Pop().BigInt()
		v.estack.PushVal(x.Neg(x))

	case ABS:
		x := v.estack.Pop().BigInt()
		v.estack.PushVal(x.Abs(x))

	case NOT:
		x := v.estack.Pop().Bool()
		v.estack.PushVal(!x)

	case NZ:
		x := v.estack.Pop().BigInt()
		v.estack.PushVal(x.Cmp(big.NewInt(0)) != 0)

	// Object operations.
	case NEWARRAY:
		item := v.estack.Pop()
		switch t := item.value.(type) {
		case *StructItem:
			arr := make([]StackItem, len(t.value))
			copy(arr, t.value)
			v.estack.PushVal(&ArrayItem{arr})
		case *ArrayItem:
			v.estack.PushVal(t)
		default:
			n := item.BigInt().Int64()
			if n > MaxArraySize {
				panic("too long array")
			}
			items := makeArrayOfFalses(int(n))
			v.estack.PushVal(&ArrayItem{items})
		}

	case NEWSTRUCT:
		item := v.estack.Pop()
		switch t := item.value.(type) {
		case *ArrayItem:
			arr := make([]StackItem, len(t.value))
			copy(arr, t.value)
			v.estack.PushVal(&StructItem{arr})
		case *StructItem:
			v.estack.PushVal(t)
		default:
			n := item.BigInt().Int64()
			if n > MaxArraySize {
				panic("too long struct")
			}
			items := makeArrayOfFalses(int(n))
			v.estack.PushVal(&StructItem{items})
		}

	case APPEND:
		itemElem := v.estack.Pop()
		arrElem := v.estack.Pop()

		val := cloneIfStruct(itemElem.value)

		switch t := arrElem.value.(type) {
		case *ArrayItem:
			arr := t.Value().([]StackItem)
			if len(arr) >= MaxArraySize {
				panic("too long array")
			}
			arr = append(arr, val)
			t.value = arr
		case *StructItem:
			arr := t.Value().([]StackItem)
			if len(arr) >= MaxArraySize {
				panic("too long struct")
			}
			arr = append(arr, val)
			t.value = arr
		default:
			panic("APPEND: not of underlying type Array")
		}

		v.estack.updateSizeAdd(val)

	case PACK:
		n := int(v.estack.Pop().BigInt().Int64())
		if n < 0 || n > v.estack.Len() || n > MaxArraySize {
			panic("OPACK: invalid length")
		}

		items := make([]StackItem, n)
		for i := 0; i < n; i++ {
			items[i] = v.estack.Pop().value
		}

		v.estack.PushVal(items)

	case UNPACK:
		a := v.estack.Pop().Array()
		l := len(a)
		for i := l - 1; i >= 0; i-- {
			v.estack.PushVal(a[i])
		}
		v.estack.PushVal(l)

	case PICKITEM:
		key := v.estack.Pop()
		validateMapKey(key)

		obj := v.estack.Pop()
		index := int(key.BigInt().Int64())

		switch t := obj.value.(type) {
		// Struct and Array items have their underlying value as []StackItem.
		case *ArrayItem, *StructItem:
			arr := t.Value().([]StackItem)
			if index < 0 || index >= len(arr) {
				panic("PICKITEM: invalid index")
			}
			item := arr[index]
			v.estack.PushVal(item)
		case *MapItem:
			if !t.Has(key.value) {
				panic("invalid key")
			}
			k := toMapKey(key.value)
			v.estack.Push(&Element{value: t.value[k]})
		default:
			arr := obj.Bytes()
			if index < 0 || index >= len(arr) {
				panic("PICKITEM: invalid index")
			}
			item := arr[index]
			v.estack.PushVal(int(item))
		}

	case SETITEM:
		item := v.estack.Pop().value
		key := v.estack.Pop()
		validateMapKey(key)

		obj := v.estack.Pop()

		switch t := obj.value.(type) {
		// Struct and Array items have their underlying value as []StackItem.
		case *ArrayItem, *StructItem:
			arr := t.Value().([]StackItem)
			index := int(key.BigInt().Int64())
			if index < 0 || index >= len(arr) {
				panic("SETITEM: invalid index")
			}
			v.estack.updateSizeRemove(arr[index])
			arr[index] = item
			v.estack.updateSizeAdd(arr[index])
		case *MapItem:
			if t.Has(key.value) {
				v.estack.updateSizeRemove(item)
			} else if len(t.value) >= MaxArraySize {
				panic("too big map")
			}
			t.Add(key.value, item)
			v.estack.updateSizeAdd(item)

		default:
			panic(fmt.Sprintf("SETITEM: invalid item type %s", t))
		}

	case REVERSE:
		a := v.estack.Pop().Array()
		if len(a) > 1 {
			for i, j := 0, len(a)-1; i <= j; i, j = i+1, j-1 {
				a[i], a[j] = a[j], a[i]
			}
		}
	case REMOVE:
		key := v.estack.Pop()
		validateMapKey(key)

		elem := v.estack.Pop()
		switch t := elem.value.(type) {
		case *ArrayItem:
			a := t.value
			k := int(key.BigInt().Int64())
			if k < 0 || k >= len(a) {
				panic("REMOVE: invalid index")
			}
			v.estack.updateSizeRemove(a[k])
			a = append(a[:k], a[k+1:]...)
			t.value = a
		case *StructItem:
			a := t.value
			k := int(key.BigInt().Int64())
			if k < 0 || k >= len(a) {
				panic("REMOVE: invalid index")
			}
			v.estack.updateSizeRemove(a[k])
			a = append(a[:k], a[k+1:]...)
			t.value = a
		case *MapItem:
			m := t.value
			k := toMapKey(key.value)
			v.estack.updateSizeRemove(m[k])
			delete(m, k)
		default:
			panic("REMOVE: invalid type")
		}

	case ARRAYSIZE:
		elem := v.estack.Pop()
		// Cause there is no native (byte) item type here, hence we need to check
		// the type of the item for array size operations.
		switch t := elem.Value().(type) {
		case []StackItem:
			v.estack.PushVal(len(t))
		case map[interface{}]StackItem:
			v.estack.PushVal(len(t))
		default:
			v.estack.PushVal(len(elem.Bytes()))
		}

	case SIZE:
		elem := v.estack.Pop()
		arr := elem.Bytes()
		v.estack.PushVal(len(arr))

	case JMP, JMPIF, JMPIFNOT:
		var (
			rOffset = int16(binary.LittleEndian.Uint16(parameter))
			offset  = ctx.ip + int(rOffset)
		)
		if offset < 0 || offset > len(ctx.prog) {
			panic(fmt.Sprintf("JMP: invalid offset %d ip at %d", offset, ctx.ip))
		}
		cond := true
		if op > JMP {
			cond = v.estack.Pop().Bool()
			if op == JMPIFNOT {
				cond = !cond
			}
		}
		if cond {
			ctx.nextip = offset
		}

	case CALL:
		v.checkInvocationStackSize()

		newCtx := ctx.Copy()
		newCtx.rvcount = -1
		v.istack.PushVal(newCtx)
		err = v.execute(v.Context(), JMP, parameter)
		if err != nil {
			return
		}

	case SYSCALL:
		ifunc, ok := v.interop[string(parameter)]
		if !ok {
			panic(fmt.Sprintf("interop hook (%q) not registered", parameter))
		}
		if err := ifunc.Func(v); err != nil {
			panic(fmt.Sprintf("failed to invoke syscall: %s", err))
		}

	case APPCALL, TAILCALL:
		if v.getScript == nil {
			panic("no getScript callback is set up")
		}

		if op == APPCALL {
			v.checkInvocationStackSize()
		}

		hash, err := util.Uint160DecodeBytes(parameter)
		if err != nil {
			panic(err)
		}

		script := v.getScript(hash)
		if script == nil {
			panic("could not find script")
		}

		if op == TAILCALL {
			_ = v.istack.Pop()
		}

		v.LoadScript(script)

	case RET:
		oldCtx := v.istack.Pop().Value().(*Context)
		rvcount := oldCtx.rvcount
		oldEstack := v.estack

		if rvcount > 0 && oldEstack.Len() < rvcount {
			panic("missing some return elements")
		}
		if v.istack.Len() == 0 {
			v.state = haltState
			break
		}

		newEstack := v.Context().estack
		if oldEstack != newEstack {
			if rvcount < 0 {
				rvcount = oldEstack.Len()
			}
			for i := rvcount; i > 0; i-- {
				elem := oldEstack.RemoveAt(i - 1)
				newEstack.Push(elem)
			}
			v.estack = newEstack
			v.astack = v.Context().astack
		}

	case CHECKSIG, VERIFY:
		var hashToCheck []byte

		keyb := v.estack.Pop().Bytes()
		signature := v.estack.Pop().Bytes()
		if op == CHECKSIG {
			if v.checkhash == nil {
				panic("VM is not set up properly for signature checks")
			}
			hashToCheck = v.checkhash
		} else { // VERIFY
			msg := v.estack.Pop().Bytes()
			hashToCheck = hash.Sha256(msg).Bytes()
		}
		pkey := &keys.PublicKey{}
		err := pkey.DecodeBytes(keyb)
		if err != nil {
			panic(err.Error())
		}
		res := pkey.Verify(signature, hashToCheck)
		v.estack.PushVal(res)

	case CHECKMULTISIG:
		pkeys, err := v.estack.popSigElements()
		if err != nil {
			panic(fmt.Sprintf("wrong parameters: %s", err.Error()))
		}
		sigs, err := v.estack.popSigElements()
		if err != nil {
			panic(fmt.Sprintf("wrong parameters: %s", err.Error()))
		}
		// It's ok to have more keys than there are signatures (it would
		// just mean that some keys didn't sign), but not the other way around.
		if len(pkeys) < len(sigs) {
			panic("more signatures than there are keys")
		}
		if v.checkhash == nil {
			panic("VM is not set up properly for signature checks")
		}
		sigok := true
		// j counts keys and i counts signatures.
		j := 0
		for i := 0; sigok && j < len(pkeys) && i < len(sigs); {
			pkey := &keys.PublicKey{}
			err := pkey.DecodeBytes(pkeys[j])
			if err != nil {
				panic(err.Error())
			}
			// We only move to the next signature if the check was
			// successful, but if it's not maybe the next key will
			// fit, so we always move to the next key.
			if pkey.Verify(sigs[i], v.checkhash) {
				i++
			}
			j++
			// When there are more signatures left to check than
			// there are keys the check won't successed for sure.
			if len(sigs)-i > len(pkeys)-j {
				sigok = false
			}
		}
		v.estack.PushVal(sigok)

	case NEWMAP:
		v.estack.Push(&Element{value: NewMapItem()})

	case KEYS:
		item := v.estack.Pop()
		if item == nil {
			panic("no argument")
		}

		m, ok := item.value.(*MapItem)
		if !ok {
			panic("not a Map")
		}

		arr := make([]StackItem, 0, len(m.value))
		for k := range m.value {
			arr = append(arr, makeStackItem(k))
		}
		v.estack.PushVal(arr)

	case VALUES:
		item := v.estack.Pop()
		if item == nil {
			panic("no argument")
		}

		var arr []StackItem
		switch t := item.value.(type) {
		case *ArrayItem, *StructItem:
			src := t.Value().([]StackItem)
			arr = make([]StackItem, len(src))
			for i := range src {
				arr[i] = cloneIfStruct(src[i])
			}
		case *MapItem:
			arr = make([]StackItem, 0, len(t.value))
			for k := range t.value {
				arr = append(arr, cloneIfStruct(t.value[k]))
			}
		default:
			panic("not a Map, Array or Struct")
		}

		v.estack.PushVal(arr)

	case HASKEY:
		key := v.estack.Pop()
		validateMapKey(key)

		c := v.estack.Pop()
		if c == nil {
			panic("no value found")
		}
		switch t := c.value.(type) {
		case *ArrayItem, *StructItem:
			index := key.BigInt().Int64()
			if index < 0 {
				panic("negative index")
			}
			v.estack.PushVal(index < int64(len(c.Array())))
		case *MapItem:
			v.estack.PushVal(t.Has(key.value))
		default:
			panic("wrong collection type")
		}

	// Cryptographic operations.
	case SHA1:
		b := v.estack.Pop().Bytes()
		sha := sha1.New()
		sha.Write(b)
		v.estack.PushVal(sha.Sum(nil))

	case SHA256:
		b := v.estack.Pop().Bytes()
		v.estack.PushVal(hash.Sha256(b).Bytes())

	case HASH160:
		b := v.estack.Pop().Bytes()
		v.estack.PushVal(hash.Hash160(b).Bytes())

	case HASH256:
		b := v.estack.Pop().Bytes()
		v.estack.PushVal(hash.DoubleSha256(b).Bytes())

	case NOP:
		// unlucky ^^

	case CALLI, CALLE, CALLED, CALLET, CALLEDT:
		var (
			tailCall    = (op == CALLET || op == CALLEDT)
			hashOnStack = (op == CALLED || op == CALLEDT)
			addElement  int
			newCtx      *Context
		)

		if hashOnStack {
			addElement = 1
		}

		rvcount := int(parameter[0])
		pcount := int(parameter[1])
		if v.estack.Len() < pcount+addElement {
			panic("missing some parameters")
		}
		if tailCall {
			if ctx.rvcount != rvcount {
				panic("context and parameter rvcount mismatch")
			}
		} else {
			v.checkInvocationStackSize()
		}

		if op == CALLI {
			newCtx = ctx.Copy()
		} else {
			var hashBytes []byte

			if hashOnStack {
				hashBytes = v.estack.Pop().Bytes()
			} else {
				hashBytes = parameter[2:]
			}

			hash, err := util.Uint160DecodeBytes(hashBytes)
			if err != nil {
				panic(err)
			}
			script := v.getScript(hash)
			if script == nil {
				panic(fmt.Sprintf("could not find script %s", hash))
			}
			newCtx = NewContext(script)
		}
		newCtx.rvcount = rvcount
		newCtx.estack = NewStack("evaluation")
		newCtx.astack = NewStack("alt")
		// Going backwards to naturally push things onto the new stack.
		for i := pcount; i > 0; i-- {
			elem := v.estack.RemoveAt(i - 1)
			newCtx.estack.Push(elem)
		}
		if tailCall {
			_ = v.istack.Pop()
		}
		v.istack.PushVal(newCtx)
		v.estack = newCtx.estack
		v.astack = newCtx.astack
		if op == CALLI {
			err = v.execute(v.Context(), JMP, parameter[2:])
			if err != nil {
				return
			}
		}

	case THROW:
		panic("THROW")

	case THROWIFNOT:
		if !v.estack.Pop().Bool() {
			panic("THROWIFNOT")
		}

	default:
		panic(fmt.Sprintf("unknown opcode %s", op.String()))
	}
	return
}

func cloneIfStruct(item StackItem) StackItem {
	switch it := item.(type) {
	case *StructItem:
		return it.Clone()
	default:
		return it
	}
}

func makeArrayOfFalses(n int) []StackItem {
	items := make([]StackItem, n)
	for i := range items {
		items[i] = &BoolItem{false}
	}
	return items
}

func validateMapKey(key *Element) {
	if key == nil {
		panic("no key found")
	}
	switch key.value.(type) {
	case *ArrayItem, *StructItem, *MapItem:
		panic("key can't be a collection")
	}
}

func (v *VM) checkInvocationStackSize() {
	if v.istack.len >= MaxInvocationStackSize {
		panic("invocation stack is too big")
	}
}

func (v *VM) checkBigIntSize(a *big.Int) {
	if a.BitLen() > MaxBigIntegerSizeBits {
		panic("big integer is too big")
	}
}
