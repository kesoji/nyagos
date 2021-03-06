package mains

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"unsafe"

	"github.com/zetamatta/nyagos/completion"
	"github.com/zetamatta/nyagos/history"
	"github.com/zetamatta/nyagos/lua"
	ole "github.com/zetamatta/nyagos/lua/ole"
	"github.com/zetamatta/nyagos/readline"
	"github.com/zetamatta/nyagos/shell"
)

const REGKEY_INTERPRETER = "nyagos.interpreter"

func setRegInt(L lua.Lua, it *shell.Cmd) {
	L.PushValue(lua.LUA_REGISTRYINDEX)
	L.PushLightUserData(unsafe.Pointer(it))
	L.SetField(-2, REGKEY_INTERPRETER)
	L.Pop(1)
}

func getRegInt(L lua.Lua) *shell.Cmd {
	L.PushValue(lua.LUA_REGISTRYINDEX)
	L.GetField(-1, REGKEY_INTERPRETER)
	rc := (*shell.Cmd)(L.ToUserData(-1))
	L.Pop(2)
	return rc
}

func NyagosCallLua(L lua.Lua, it *shell.Cmd, nargs int, nresult int) error {
	save := getRegInt(L)
	setRegInt(L, it)
	err := L.Call(nargs, nresult)
	setRegInt(L, save)
	return err
}

var orgArgHook func(*shell.Cmd, []string) ([]string, error)

var luaArgsFilter lua.Object = lua.TNil{}

func newArgHook(it *shell.Cmd, args []string) ([]string, error) {
	L, ok := it.Tag.(lua.Lua)
	if !ok {
		return nil, errors.New("Could not get lua instance(newArgHook)")
	}
	L.Push(luaArgsFilter)
	if !L.IsFunction(-1) {
		L.Pop(1)
		return orgArgHook(it, args)
	}
	L.NewTable()
	for i := 0; i < len(args); i++ {
		L.PushString(args[i])
		L.RawSetI(-2, lua.Integer(i))
	}
	if err := L.Call(1, 1); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return orgArgHook(it, args)
	}
	if L.GetType(-1) != lua.LUA_TTABLE {
		return orgArgHook(it, args)
	}
	newargs := []string{}
	for i := lua.Integer(0); true; i++ {
		L.PushInteger(i)
		L.GetTable(-2)
		if L.GetType(-1) == lua.LUA_TNIL {
			break
		}
		arg1, arg1err := L.ToString(-1)
		if arg1err == nil {
			newargs = append(newargs, arg1)
		} else {
			fmt.Fprintln(os.Stderr, arg1err.Error())
		}
		L.Pop(1)
	}
	return orgArgHook(it, newargs)
}

var orgOnCommandNotFound func(*shell.Cmd, error) error

var luaOnCommandNotFound lua.Object = lua.TNil{}

func on_command_not_found(inte *shell.Cmd, err error) error {
	L, ok := inte.Tag.(lua.Lua)
	if !ok {
		return errors.New("Could get lua instance(on_command_not_found)")
	}

	L.Push(luaOnCommandNotFound)
	if !L.IsFunction(-1) {
		L.Pop(1)
		return orgOnCommandNotFound(inte, err)
	}
	L.NewTable()
	for key, val := range inte.Args {
		L.PushString(val)
		L.RawSetI(-2, lua.Integer(key))
	}
	err1 := L.Call(1, 1)
	defer L.Pop(1)
	if err1 != nil {
		return err
	}
	if L.ToBool(-1) {
		return nil
	} else {
		return orgOnCommandNotFound(inte, err)
	}
}

var option_table_member = map[string]IProperty{
	"glob": &lua.BoolProperty{Pointer: &shell.WildCardExpansionAlways},
}

func getOption(L lua.Lua) int {
	key, key_err := L.ToString(2)
	if key_err != nil {
		return L.Push(nil, key_err)
	}
	val, val_ok := option_table_member[key]
	if !val_ok {
		return L.Push(nil)
	}
	return L.Push(val)
}

func setOption(L lua.Lua) int {
	key, key_err := L.ToString(2)
	if key_err != nil {
		return L.Push(nil, key_err)
	}
	opt, opt_ok := option_table_member[key]
	if !opt_ok {
		print(key, " not found\n")
		return L.Push(nil)
	}
	if err := opt.Set(L, 3); err != nil {
		return L.Push(nil, err.Error())
	} else {
		return L.Push(true)
	}
}

var nyagos_table_member map[string]lua.Object

func getNyagosTable(L lua.Lua) int {
	index, index_err := L.ToString(2)
	if index_err != nil {
		return L.Push(nil, index_err.Error())
	}
	if entry, entry_ok := nyagos_table_member[index]; entry_ok {
		return L.Push(entry)
	} else if index == "exe" {
		if exeName, exeNameErr := os.Executable(); exeNameErr != nil {
			return L.Push(nil, exeNameErr.Error())
		} else {
			L.PushString(exeName)
			return 1
		}
	} else {
		L.PushNil()
		return 1
	}
}

type IProperty interface {
	Push(lua.Lua) int
	Set(lua.Lua, int) error
}

func setNyagosTable(L lua.Lua) int {
	index, index_err := L.ToString(2)
	if index_err != nil {
		return L.Push(nil, index_err)
	}
	if current_value, exists := nyagos_table_member[index]; exists {
		if property, castOk := current_value.(IProperty); castOk {
			if err := property.Set(L, 3); err != nil {
				fmt.Fprintf(os.Stderr, "nyagos.%s: %s\n", index, err.Error())
				return L.Push(nil, err)
			} else {
				return L.Push(true)
			}
		} else {
			value, value_err := L.ToObject(3)
			if value_err != nil {
				return L.Push(nil, value_err)
			}
			nyagos_table_member[index] = value
			return L.Push(true)
		}
	} else {
		fmt.Fprintf(os.Stderr, "nyagos.%s: reserved variable.\n", index)
		return L.Push(nil)
	}
}

var share_table = map[string]lua.Object{}
var share_table_generation = map[string]int{}

func setMemberOfShareTable(L lua.Lua) int {
	// table exists at [-3]
	key, err := L.ToObject(-2)
	if err != nil {
		return L.Push(nil, err)
	}
	val, err := L.ToObject(-1)
	if err != nil {
		return L.Push(nil, err)
	}
	L.RawSet(-3) // pop 2
	L.GetMetaTable(-1)
	L.GetField(-1, "..")
	parentkey, err := L.ToString(-1)
	if err != nil {
		println(err.Error())
		return L.Push(nil, err)
	}
	L.Pop(1) // drop string
	L.GetField(-1, "age")
	age, err := L.ToInteger(-1)
	L.Pop(2) // drop integer and metatable

	if err != nil || age != share_table_generation[parentkey] {
		// println("old variable")
		return 0
	}

	table1, ok := share_table[parentkey]
	if !ok {
		err := fmt.Errorf("%s: not found in share_table()", parentkey)
		println(err.Error())
		return L.Push(nil, err.Error())
	}
	if t, ok := table1.(*lua.MetaTableOwner); ok {
		table1 = t.Body
	}
	table2, ok := table1.(*lua.TTable)
	if !ok {
		err := fmt.Errorf("%s: not table in share_table()", parentkey)
		type1 := reflect.TypeOf(table1)
		println(type1.String())
		println(err.Error())
		return L.Push(nil, err.Error())
	}
	switch t := key.(type) {
	case lua.TString:
		table2.Dict[string(t)] = val
	case lua.TRawString:
		table2.Dict[string(t)] = val
	case lua.Integer:
		table2.Array[int(t)] = val
	}
	return 0
}

func getShareTable(L lua.Lua) int {
	key, keyErr := L.ToString(-1)
	if keyErr != nil {
		return L.Push(nil, keyErr)
	}
	if value, ok := share_table[key]; ok {
		L.Push(value)
		if L.IsTable(-1) {
			L.NewTable()
			L.PushGoFunction(setMemberOfShareTable)
			L.SetField(-2, "__newindex")
			L.PushString(key)
			L.SetField(-2, "..")
			L.PushInteger(lua.Integer(share_table_generation[key]))
			L.SetField(-2, "age")
			L.SetMetaTable(-2)
		}
		return 1

	} else {
		L.PushNil()
		return 1
	}
}

func setShareTable(L lua.Lua) int {
	key, keyErr := L.ToString(-2)
	if keyErr != nil {
		return L.Push(nil, keyErr)
	}
	value, valErr := L.ToObject(-1)
	if valErr != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", key, valErr.Error())
		return L.Push(nil, valErr)
	}
	share_table[key] = value
	share_table_generation[key]++
	return 1
}

var hook_setuped = false

func NewLua() (lua.Lua, error) {
	this, err := lua.New()
	if err != nil {
		return this, err
	}
	this.OpenLibs()

	this.Push(&lua.VirtualTable{
		Name:     "nyagos",
		Index:    getNyagosTable,
		NewIndex: setNyagosTable})
	this.SetGlobal("nyagos")

	this.Push(&lua.VirtualTable{
		Name:     "share",
		Index:    getShareTable,
		NewIndex: setShareTable})
	this.SetGlobal("share")

	if !hook_setuped {
		orgArgHook = shell.SetArgsHook(newArgHook)

		orgOnCommandNotFound = shell.OnCommandNotFound
		shell.OnCommandNotFound = on_command_not_found
		hook_setuped = true
	}
	return this, nil
}

var silentmode = false

func lua2cmd(f func([]interface{}) []interface{}) func(lua.Lua) int {
	return func(L lua.Lua) int {
		end := L.GetTop()
		var param []interface{}
		if end > 0 {
			param = make([]interface{}, 0, end-1)
			for i := 1; i <= end; i++ {
				value, _ := L.ToInterface(i)
				param = append(param, value)
			}
		} else {
			param = []interface{}{}
		}
		result := f(param)
		for _, value := range result {
			L.PushReflect(value)
		}
		return len(result)
	}
}

func init() {
	nyagos_table_member = map[string]lua.Object{
		"access": lua.TGoFunction(cmdAccess),
		"alias": &lua.VirtualTable{
			Name:     "nyagos.alias",
			Index:    cmdGetAlias,
			NewIndex: cmdSetAlias},
		"antihistquot": lua.StringProperty{Pointer: &history.DisableMarks},
		"argsfilter":   lua.Property{Pointer: &luaArgsFilter},
		"atou":         lua.TGoFunction(cmdAtoU),
		"key": &lua.VirtualTable{
			Name:     "nyagos.key",
			Index:    cmdGetBindKey,
			NewIndex: cmdBindKey},
		"bindkey":           lua.TGoFunction(cmdBindKey),
		"box":               lua.TGoFunction(cmdBox),
		"chdir":             lua.TGoFunction(lua2cmd(cmdChdir)),
		"commit":            lua.StringProperty{Pointer: &Commit},
		"commonprefix":      lua.TGoFunction(cmdCommonPrefix),
		"completion_slash":  lua.BoolProperty{Pointer: &completion.UseSlash},
		"completion_hook":   lua.Property{Pointer: &completionHook},
		"completion_hidden": lua.BoolProperty{Pointer: &completion.IncludeHidden},
		"create_object":     lua.TGoFunction(ole.CreateObject),
		"default_prompt":    lua.TGoFunction(nyagosPrompt),
		"elevated":          lua.TGoFunction(lua2cmd(cmdElevated)),
		"env": &lua.VirtualTable{
			Name:     "nyagos.env",
			Index:    cmdGetEnv,
			NewIndex: cmdSetEnv},
		"eval":         lua.TGoFunction(cmdEval),
		"exec":         lua.TGoFunction(cmdExec),
		"filter":       lua.Property{Pointer: &luaFilter},
		"getalias":     lua.TGoFunction(cmdGetAlias),
		"getenv":       lua.TGoFunction(cmdGetEnv),
		"gethistory":   lua.TGoFunction(cmdGetHistory),
		"getkey":       lua.TGoFunction(cmdGetKey),
		"getviewwidth": lua.TGoFunction(cmdGetViewWidth),
		"getwd":        lua.TGoFunction(cmdGetwd),
		"glob":         lua.TGoFunction(cmdGlob),
		"goarch":       lua.TString(runtime.GOARCH),
		"goversion":    lua.TString(runtime.Version()),
		"histchar":     lua.StringProperty{Pointer: &history.Mark},
		"history": &lua.VirtualTable{
			Name:  "nyagos.history",
			Index: cmdGetHistory,
			Len:   cmdLenHistory},
		"lines":                lua.TGoFunction(cmdLines),
		"loadfile":             lua.TGoFunction(cmdLoadFile),
		"netdrivetounc":        lua.TGoFunction(lua2cmd(cmdNetDriveToUNC)),
		"on_command_not_found": lua.Property{Pointer: &luaOnCommandNotFound},
		"open":                 lua.TGoFunction(cmdOpenFile),
		"option": &lua.VirtualTable{
			Name:     "nyagos.option",
			Index:    getOption,
			NewIndex: setOption},
		"pathjoin":       lua.TGoFunction(cmdPathJoin),
		"prompt":         lua.Property{Pointer: &prompt_hook},
		"quotation":      lua.StringProperty{Pointer: &readline.Delimiters},
		"raweval":        lua.TGoFunction(cmdRawEval),
		"rawexec":        lua.TGoFunction(cmdRawExec),
		"resetcharwidth": lua.TGoFunction(lua2cmd(cmdResetCharWidth)),
		"setalias":       lua.TGoFunction(cmdSetAlias),
		"setenv":         lua.TGoFunction(cmdSetEnv),
		"setrunewidth":   lua.TGoFunction(cmdSetRuneWidth),
		"shellexecute":   lua.TGoFunction(cmdShellExecute),
		"silentmode":     &lua.BoolProperty{Pointer: &silentmode},
		"stamp":          lua.StringProperty{Pointer: &Stamp},
		"stat":           lua.TGoFunction(cmdStat),
		"utoa":           lua.TGoFunction(cmdUtoA),
		"version":        lua.StringProperty{Pointer: &Version},
		"which":          lua.TGoFunction(cmdWhich),
		"write":          lua.TGoFunction(cmdWrite),
		"writerr":        lua.TGoFunction(cmdWriteErr),
	}
}
