// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package ghttp

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gogf/gf/container/gpool"
	"github.com/gogf/gf/os/gfile"
	"github.com/gogf/gf/text/gregex"
	"github.com/gogf/gf/text/gstr"
)

var serviceMultiObjectCache map[string]*gpool.Pool

// var objectCache map[string][]*objectCacheInfo

type serviceMultiObjectInfo struct {
	rVal    reflect.Value
	methods map[string]func(*Request)

	initFunc func(*Request)
	shutFunc func(*Request)
}

func init() {
	serviceMultiObjectCache = make(map[string]*gpool.Pool, 0)
	// objectCache = make(map[string][]*objectCacheInfo, 0)
}

// BindObject registers object to server routes with given pattern.
//
// The optional parameter <method> is used to specify the method to be registered, which
// supports multiple method names, multiple methods are separated by char ',', case sensitive.
//
// Note that the route method should be defined as ghttp.HandlerFunc.
func (s *Server) BindMultiObject(pattern string, object interface{}, method ...string) {
	bindMethod := ""
	if len(method) > 0 {
		bindMethod = method[0]
	}
	s.doBindMultiObject(pattern, object, bindMethod, nil, "")
}

// BindObjectMethod registers specified method of object to server routes with given pattern.
//
// The optional parameter <method> is used to specify the method to be registered, which
// does not supports multiple method names but only one, case sensitive.
//
// Note that the route method should be defined as ghttp.HandlerFunc.
func (s *Server) BindMultiObjectMethod(pattern string, object interface{}, method string) {
	s.doBindMultiObjectMethod(pattern, object, method, nil, "")
}

// BindObjectRest registers object in REST API style to server with specified pattern.
// Note that the route method should be defined as ghttp.HandlerFunc.
func (s *Server) BindMultiObjectRest(pattern string, object interface{}) {
	s.doBindMultiObjectRest(pattern, object, nil, "")
}

func (s *Server) callMultiObjectMethods(object interface{}, methodName string) func(*Request) {

	var (
		v = reflect.ValueOf(object)
		t = v.Type()
	)

	structName := t.Elem().Name()

	return func(r *Request) {

		pool, ok := serviceMultiObjectCache[structName]
		if !ok {
			pool = gpool.New(time.Minute*5, func() (interface{}, error) {
				v := reflect.ValueOf(object)
				t := v.Type()

				o := new(serviceMultiObjectInfo)
				o.rVal = reflect.New(t)
				ov := o.rVal.Elem()
				ov.Set(v)
				o.methods = make(map[string]func(*Request), 0)

				if ov.MethodByName("Init").IsValid() {
					o.initFunc = ov.MethodByName("Init").Interface().(func(*Request))
				}
				if ov.MethodByName("Shut").IsValid() {
					o.shutFunc = ov.MethodByName("Shut").Interface().(func(*Request))
				}
				return o, nil
			})
			serviceMultiObjectCache[structName] = pool
		}

		po, _ := pool.Get()
		o := po.(*serviceMultiObjectInfo)

		if o.initFunc != nil {
			niceCallFunc(func() {
				o.initFunc(r)
			})
		}

		if itemFunc, ok := o.methods[methodName]; !ok {
			v := o.rVal
			ov := v.Elem()
			methodValue := ov.MethodByName(methodName)
			if itemFunc, ok := methodValue.Interface().(func(*Request)); ok {
				o.methods[methodName] = itemFunc
				niceCallFunc(func() {
					itemFunc(r)
				})
			}
		} else {
			niceCallFunc(func() {
				itemFunc(r)
			})
		}

		if o.shutFunc != nil {
			niceCallFunc(func() {
				o.shutFunc(r)
			})
		}

		pool.Put(o)
	}
}

func (s *Server) doBindMultiObject(
	pattern string, object interface{}, method string,
	middleware []HandlerFunc, source string,
) {
	// Convert input method to map for convenience and high performance searching purpose.
	var methodMap map[string]bool
	if len(method) > 0 {
		methodMap = make(map[string]bool)
		for _, v := range strings.Split(method, ",") {
			methodMap[strings.TrimSpace(v)] = true
		}
	}
	// If the `method` in `pattern` is `defaultMethod`,
	// it removes for convenience for next statement control.
	domain, method, path, err := s.parsePattern(pattern)
	if err != nil {
		s.Logger().Fatal(err)
		return
	}
	if strings.EqualFold(method, defaultMethod) {
		pattern = s.serveHandlerKey("", path, domain)
	}
	var (
		m = make(map[string]*handlerItem)
		v = reflect.ValueOf(object)
		t = v.Type()
		// initFunc func(*Request)
		// shutFunc func(*Request)
	)
	// If given `object` is not pointer, it then creates a temporary one,
	// of which the value is `v`.
	if v.Kind() == reflect.Struct {
		newValue := reflect.New(t)
		newValue.Elem().Set(v)
		v = newValue
		t = v.Type()
	}
	structName := t.Elem().Name()
	// if v.MethodByName("Init").IsValid() {
	// 	initFunc = v.MethodByName("Init").Interface().(func(*Request))
	// }
	// if v.MethodByName("Shut").IsValid() {
	// 	shutFunc = v.MethodByName("Shut").Interface().(func(*Request))
	// }
	pkgPath := t.Elem().PkgPath()
	pkgName := gfile.Basename(pkgPath)
	for i := 0; i < v.NumMethod(); i++ {
		methodName := t.Method(i).Name
		if methodMap != nil && !methodMap[methodName] {
			continue
		}
		if methodName == "Init" || methodName == "Shut" {
			continue
		}
		objName := gstr.Replace(t.String(), fmt.Sprintf(`%s.`, pkgName), "")
		if objName[0] == '*' {
			objName = fmt.Sprintf(`(%s)`, objName)
		}
		_, ok := v.Method(i).Interface().(func(*Request))
		if !ok {
			if len(methodMap) > 0 {
				s.Logger().Errorf(
					`invalid route method: %s.%s.%s defined as "%s", but "func(*ghttp.Request)" is required for object registry`,
					pkgPath, objName, methodName, v.Method(i).Type().String(),
				)
			} else {
				s.Logger().Debugf(
					`ignore route method: %s.%s.%s defined as "%s", no match "func(*ghttp.Request)" for object registry`,
					pkgPath, objName, methodName, v.Method(i).Type().String(),
				)
			}
			continue
		}
		key := s.mergeBuildInNameToPattern(pattern, structName, methodName, true)
		m[key] = &handlerItem{
			itemName: fmt.Sprintf(`%s.%s.%s`, pkgPath, objName, methodName),
			itemType: handlerTypeHandler, //  handlerTypeObject,
			itemFunc: s.callMultiObjectMethods(object, methodName),
			// itemFunc: itemFunc,
			// initFunc:   initFunc,
			// shutFunc:   shutFunc,
			middleware: middleware,
			source:     source,
		}
		// If there's "Index" method, then an additional route is automatically added
		// to match the main URI, for example:
		// If pattern is "/user", then "/user" and "/user/index" are both automatically
		// registered.
		//
		// Note that if there's built-in variables in pattern, this route will not be added
		// automatically.
		if strings.EqualFold(methodName, "Index") && !gregex.IsMatchString(`\{\.\w+\}`, pattern) {
			p := gstr.PosRI(key, "/index")
			k := key[0:p] + key[p+6:]
			if len(k) == 0 || k[0] == '@' {
				k = "/" + k
			}
			m[k] = &handlerItem{
				itemName: fmt.Sprintf(`%s.%s.%s`, pkgPath, objName, methodName),
				itemType: handlerTypeHandler, //   handlerTypeObject,
				itemFunc: s.callMultiObjectMethods(object, methodName),
				// initFunc:   initFunc,
				// shutFunc:   shutFunc,
				middleware: middleware,
				source:     source,
			}
		}
	}
	s.bindHandlerByMap(m)
}

func (s *Server) doBindMultiObjectMethod(
	pattern string, object interface{}, method string,
	middleware []HandlerFunc, source string,
) {
	var (
		m = make(map[string]*handlerItem)
		v = reflect.ValueOf(object)
		t = v.Type()
		// initFunc func(*Request)
		// shutFunc func(*Request)
	)
	// If given `object` is not pointer, it then creates a temporary one,
	// of which the value is `v`.
	if v.Kind() == reflect.Struct {
		newValue := reflect.New(t)
		newValue.Elem().Set(v)
		v = newValue
		t = v.Type()
	}
	structName := t.Elem().Name()
	methodName := strings.TrimSpace(method)
	methodValue := v.MethodByName(methodName)
	if !methodValue.IsValid() {
		s.Logger().Fatal("invalid method name: " + methodName)
		return
	}
	// if v.MethodByName("Init").IsValid() {
	// 	initFunc = v.MethodByName("Init").Interface().(func(*Request))
	// }
	// if v.MethodByName("Shut").IsValid() {
	// 	shutFunc = v.MethodByName("Shut").Interface().(func(*Request))
	// }
	pkgPath := t.Elem().PkgPath()
	pkgName := gfile.Basename(pkgPath)
	objName := gstr.Replace(t.String(), fmt.Sprintf(`%s.`, pkgName), "")
	if objName[0] == '*' {
		objName = fmt.Sprintf(`(%s)`, objName)
	}
	_, ok := methodValue.Interface().(func(*Request))
	if !ok {
		s.Logger().Errorf(
			`invalid route method: %s.%s.%s defined as "%s", but "func(*ghttp.Request)" is required for object registry`,
			pkgPath, objName, methodName, methodValue.Type().String(),
		)
		return
	}
	key := s.mergeBuildInNameToPattern(pattern, structName, methodName, false)
	m[key] = &handlerItem{
		itemName: fmt.Sprintf(`%s.%s.%s`, pkgPath, objName, methodName),
		itemType: handlerTypeHandler,
		itemFunc: s.callMultiObjectMethods(object, methodName),
		// itemType:   handlerTypeObject,
		// itemFunc:   itemFunc,
		// initFunc:   initFunc,
		// shutFunc:   shutFunc,
		middleware: middleware,
		source:     source,
	}

	s.bindHandlerByMap(m)
}

func (s *Server) doBindMultiObjectRest(
	pattern string, object interface{},
	middleware []HandlerFunc, source string,
) {
	var (
		m = make(map[string]*handlerItem)
		v = reflect.ValueOf(object)
		t = v.Type()
		// initFunc func(*Request)
		// shutFunc func(*Request)
	)
	// If given `object` is not pointer, it then creates a temporary one,
	// of which the value is `v`.
	if v.Kind() == reflect.Struct {
		newValue := reflect.New(t)
		newValue.Elem().Set(v)
		v = newValue
		t = v.Type()
	}
	structName := t.Elem().Name()
	// if v.MethodByName("Init").IsValid() {
	// 	initFunc = v.MethodByName("Init").Interface().(func(*Request))
	// }
	// if v.MethodByName("Shut").IsValid() {
	// 	shutFunc = v.MethodByName("Shut").Interface().(func(*Request))
	// }
	pkgPath := t.Elem().PkgPath()
	for i := 0; i < v.NumMethod(); i++ {
		methodName := t.Method(i).Name
		if _, ok := methodsMap[strings.ToUpper(methodName)]; !ok {
			continue
		}
		pkgName := gfile.Basename(pkgPath)
		objName := gstr.Replace(t.String(), fmt.Sprintf(`%s.`, pkgName), "")
		if objName[0] == '*' {
			objName = fmt.Sprintf(`(%s)`, objName)
		}
		_, ok := v.Method(i).Interface().(func(*Request))
		if !ok {
			s.Logger().Errorf(
				`invalid route method: %s.%s.%s defined as "%s", but "func(*ghttp.Request)" is required for object registry`,
				pkgPath, objName, methodName, v.Method(i).Type().String(),
			)
			continue
		}
		key := s.mergeBuildInNameToPattern(methodName+":"+pattern, structName, methodName, false)
		m[key] = &handlerItem{
			itemName: fmt.Sprintf(`%s.%s.%s`, pkgPath, objName, methodName),
			itemType: handlerTypeHandler,
			itemFunc: s.callMultiObjectMethods(object, methodName),

			// itemType:   handlerTypeObject,
			// itemFunc:   itemFunc,
			// initFunc:   initFunc,
			// shutFunc:   shutFunc,
			middleware: middleware,
			source:     source,
		}
	}
	s.bindHandlerByMap(m)
}
