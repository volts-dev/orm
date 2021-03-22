package orm

import (
	"log"
	"reflect"
)

type (
	TMethod struct {
		name   string
		args   []reflect.Value
		method reflect.Value
		result []reflect.Value
	}

	// A MethodsCollection is a collection of methods for use in a model
	// Model 的方法集
	TMethodsSet struct {
		model    *TModel
		registry map[string]*TMethod
		//powerGroups  map[*security.Group]bool
		bootstrapped bool
	}
)

func NewMethod(name string, method reflect.Value) *TMethod {
	return &TMethod{
		name: name,
		//args:   make([]reflect.Value, 0),
		method: method,
	}
}

func (self *TMethod) Name() string {
	return self.name
}

//TODO dataset
func (self *TMethod) Result() []reflect.Value {
	return self.result
}

//废弃
func (self *TMethod) ___SetArgs(args ...interface{}) bool {
	self.args = make([]reflect.Value, 0)

	for _, arg := range args {
		self.args = append(self.args, reflect.ValueOf(arg))
	}
	return true
}

func (self *TMethod) AsInterface() interface{} {
	return self.method.Interface()
}

func (self *TMethod) Call(model interface{}, args ...interface{}) bool {
	self.args = make([]reflect.Value, 0)
	self.args = append(self.args, reflect.ValueOf(model))
	
	for _, arg := range args {
		self.args = append(self.args, reflect.ValueOf(arg))
	}

	if self.method.IsValid() {
		self.result = self.method.Call(self.args)
		return true
	}

	return false
}

// get returns the Method with the given method name.
func (self *TMethodsSet) get(methodName string) (*TMethod, bool) {
	mi, ok := self.registry[methodName]
	if !ok {
		/*	// We didn't find the method, but maybe it exists in mixins
			miMethod, found := self.model.findMethodInMixin(methodName)
			if !found || self.bootstrapped {
				return nil, false
			}
			// The method exists in a mixin so we create it here with our layer.
			// Bootstrap will take care of putting them the right way round afterwards.
			mi = copyMethod(self.model, miMethod)
			self.set(methodName, mi)

		*/
	}
	return mi, true
}

// MustGet returns the Method of the given method. It panics if the
// method is not found.
func (self *TMethodsSet) MustGet(methodName string) *TMethod {
	methInfo, exists := self.get(methodName)
	if !exists {
		log.Panic("Unknown method in model", "model", self.model.GetName(), "method", methodName)
	}
	return methInfo
}

// set adds the given Method to the MethodsCollection.
func (self *TMethodsSet) set(methodName string, methInfo *TMethod) {
	self.registry[methodName] = methInfo
}
