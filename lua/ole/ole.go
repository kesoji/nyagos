package goluaole

import (
	"unsafe"

	lua "../../lua"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

var initialized_required = true

type capsule_t struct {
	Data *ole.IDispatch
}

func (this capsule_t) Push(L lua.Lua) int {
	p := (*capsule_t)(L.NewUserData(unsafe.Sizeof(this)))
	p.Data = this.Data
	L.NewTable()
	L.PushGoFunction(gc)
	L.SetField(-2, "__gc")
	L.PushGoFunction(index)
	L.SetField(-2, "__index")
	L.SetMetaTable(-2)
	return 1
}

func gc(L lua.Lua) int {
	p := (*capsule_t)(L.ToUserData(1))
	if p.Data != nil {
		p.Data.Release()
		p.Data = nil
	}
	return 0
}

func lua2interface(L lua.Lua, index int) (interface{}, error) {
	switch L.GetType(index) {
	default:
		return nil, nil
	case lua.LUA_TSTRING:
		str, str_err := L.ToString(index)
		return str, str_err
	case lua.LUA_TNUMBER:
		num, num_err := L.ToInteger(index)
		return num, num_err
	case lua.LUA_TUSERDATA:
		data := L.ToUserData(index)
		val := (*capsule_t)(data)
		return val.Data, nil
	case lua.LUA_TBOOLEAN:
		return L.ToBool(index), nil
	}
}

func lua2interfaceS(L lua.Lua, start, end int) ([]interface{}, error) {
	result := make([]interface{}, 0, end-start+1)
	for i := start; i <= end; i++ {
		val, val_err := lua2interface(L, i)
		if val_err != nil {
			return nil, val_err
		}
		result = append(result, val)
	}
	return result, nil
}

func call(L lua.Lua) int {
	p := (*capsule_t)(L.ToUserData(1))
	if p == nil {
		return L.Push(nil, "OLEOBJECT._call: the receiver is null")
	}
	count := L.GetTop()
	name, name_err := L.ToString(2)
	if name_err != nil {
		return L.Push(nil, name_err)
	}
	params, params_err := lua2interfaceS(L, 3, count)
	if params_err != nil {
		return L.Push(nil, params_err)
	}
	result, result_err := oleutil.CallMethod(p.Data, name, params...)
	if result_err != nil {
		return L.Push(nil, result_err)
	}
	if result.VT == ole.VT_DISPATCH {
		return capsule_t{result.ToIDispatch()}.Push(L)
	} else {
		return L.Push(result.Value())
	}
}

func set(L lua.Lua) int {
	p := (*capsule_t)(L.ToUserData(1))
	if p == nil {
		return L.Push(nil, "OLEOBJECT._set: the receiver is null")
	}
	name, name_err := L.ToString(2)
	if name_err != nil {
		return L.Push(nil, name_err)
	}
	key, key_err := lua2interfaceS(L, 3, L.GetTop())
	if key_err != nil {
		return L.Push(nil, key_err)
	}
	oleutil.PutProperty(p.Data, name, key...)
	return L.Push(true, nil)
}

func get(L lua.Lua) int {
	p := (*capsule_t)(L.ToUserData(1))
	if p == nil {
		return L.Push(nil, "OLEOBJECT._set: the receiver is null")
	}
	name, name_err := L.ToString(2)
	if name_err != nil {
		return L.Push(nil, name_err)
	}
	key, key_err := lua2interfaceS(L, 3, L.GetTop())
	if key_err != nil {
		return L.Push(nil, key_err)
	}
	result, result_err := oleutil.GetProperty(p.Data, name, key...)
	if result_err != nil {
		return L.Push(nil, result_err)
	}
	if result.VT == ole.VT_DISPATCH {
		return capsule_t{result.ToIDispatch()}.Push(L)
	} else {
		return L.Push(result.Value())
	}
}

func index(L lua.Lua) int {
	// p := (*capsule_t)(L.ToUserData(1))
	name, name_err := L.ToString(2)
	if name_err != nil {
		// print(name_err.Error(), "\n")
		return L.Push(nil, name_err)
	}
	switch name {
	case "_call":
		return L.Push(call, nil)
	case "_set":
		return L.Push(set, nil)
	case "_get":
		return L.Push(get, nil)
	default:
		return 0
	}
}

func CreateObject(L lua.Lua) int {
	if initialized_required {
		ole.CoInitialize(0)
		initialized_required = false
	}
	name, name_err := L.ToString(1)
	if name_err != nil {
		return L.Push(nil, name_err)
	}
	unknown, unknown_err := oleutil.CreateObject(name)
	if unknown_err != nil {
		return L.Push(nil, unknown_err)
	}
	obj, obj_err := unknown.QueryInterface(ole.IID_IDispatch)
	if obj_err != nil {
		return L.Push(nil, obj_err)
	}
	capsule_t{obj}.Push(L)
	return 1
}