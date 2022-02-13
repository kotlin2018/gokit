package gokit

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/gogf/gf/util/gvalid"
	"github.com/julienschmidt/httprouter"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"
	//"gopkg.in/yaml.v2"
	//"github.com/golang/protobuf/proto"
	//"github.com/ugorji/go/codec"
)

func filterFlags(content string) string {
	for i, char := range content {
		if char == ' ' || char == ';' {
			return content[:i]
		}
	}
	return content
}

func Parse(r *http.Request,p httprouter.Params,obj interface{},rule string,f gvalid.RuleFunc){
	contentType :=filterFlags(r.Header.Get("Content-Type"))
	if r.Method == http.MethodGet {
		if strings.Contains(r.URL.String(),"/:"){
			m := make(map[string][]string)
			for _, v := range p {
				m[v.Key] = []string{v.Value}
			}
			uriBinding{}.BindUri(m, obj,rule,f)
			return
		}else {
			formBinding{}.Bind(r, obj,rule,f)
			return
		}
	}
	switch contentType {
	case "application/json":
		jsonBinding{}.Bind(r, obj,rule,f)
		return
	case "application/xml","text/xml":
		xmlBinding{}.Bind(r, obj,rule,f)
		return
	case "multipart/form-data":
		formMultipartBinding{}.Bind(r, obj,rule,f)
		return
	//case "application/x-protobuf":
	//	protobufBinding{}.Bind(r, obj,rule,f)
	//	return
	//case "application/x-msgpack", "application/msgpack":
	//	msgpackBinding{}.Bind(r, obj,rule,f)
	//	return
	//case "application/x-yaml":
	//	yamlBinding{}.Bind(r, obj,rule,f)
	//	return
	default:
		formBinding{}.Bind(r, obj,rule,f)
		return
	}
}

type bind interface {
	Name() string
	Bind(*http.Request, interface{},string,gvalid.RuleFunc) error
}

type bindBody interface {
	bind
	BindBody([]byte, interface{},string,gvalid.RuleFunc) error
}

type bindUri interface {
	Name() string
	BindUri(map[string][]string, interface{},string,gvalid.RuleFunc) error
}

type jsonBinding struct{}

func (jsonBinding) Name() string {
	return "json"
}

func (jsonBinding) Bind(req *http.Request, obj interface{},rule string,f gvalid.RuleFunc) error {
	if req == nil || req.Body == nil {
		return fmt.Errorf("invalid request")
	}
	return decodeJSON(req.Body, obj,rule,f)
}

func (jsonBinding) BindBody(body []byte, obj interface{},rule string,f gvalid.RuleFunc) error {
	return decodeJSON(bytes.NewReader(body), obj,rule,f)
}

func decodeJSON(r io.Reader, obj interface{},rule string,f gvalid.RuleFunc) error {
	if err := json.NewDecoder(r).Decode(obj); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

type xmlBinding struct{}

func (xmlBinding) Name() string {
	return "xml"
}

func (xmlBinding) Bind(req *http.Request, obj interface{},rule string,f gvalid.RuleFunc) error {
	return decodeXML(req.Body, obj,rule,f)
}

func (xmlBinding) BindBody(body []byte, obj interface{},rule string,f gvalid.RuleFunc) error {
	return decodeXML(bytes.NewReader(body), obj,rule,f)
}
func decodeXML(r io.Reader, obj interface{},rule string,f gvalid.RuleFunc) error {
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

const defaultMemory = 32 << 20

type formBinding struct{}
type formPostBinding struct{}
type formMultipartBinding struct{}

func (formBinding) Name() string {
	return "form"
}

func (formBinding) Bind(req *http.Request, obj interface{},rule string,f gvalid.RuleFunc) error {
	if err := req.ParseForm(); err != nil {
		return err
	}
	if err := req.ParseMultipartForm(defaultMemory); err != nil {
		if err != http.ErrNotMultipart {
			return err
		}
	}
	if err := mapFormByTag(obj, req.Form,"form"); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

func (formPostBinding) Name() string {
	return "form-urlencoded"
}

func (formPostBinding) Bind(req *http.Request, obj interface{},rule string,f gvalid.RuleFunc) error {
	if err := req.ParseForm(); err != nil {
		return err
	}
	if err := mapFormByTag(obj, req.PostForm,"form"); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

func (formMultipartBinding) Name() string {
	return "multipart/form-data"
}

func (formMultipartBinding) Bind(req *http.Request, obj interface{},rule string,f gvalid.RuleFunc) error {
	if err := req.ParseMultipartForm(defaultMemory); err != nil {
		return err
	}
	if err := mappingByPtr(obj, (*multipartRequest)(req), "form"); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

func mapFormByTag(ptr interface{}, form map[string][]string, tag string) error {
	// Check if ptr is a map
	ptrVal := reflect.ValueOf(ptr)
	var pointed interface{}
	if ptrVal.Kind() == reflect.Ptr {
		ptrVal = ptrVal.Elem()
		pointed = ptrVal.Interface()
	}
	if ptrVal.Kind() == reflect.Map &&
		ptrVal.Type().Key().Kind() == reflect.String {
		if pointed != nil {
			ptr = pointed
		}
		return setFormMap(ptr, form)
	}
	return mappingByPtr(ptr, formSource(form), tag)
}

type multipartRequest http.Request

var _ setter = (*multipartRequest)(nil)

// TrySet tries to set a value by the multipart request with the binding a form file
func (r *multipartRequest) trySet(value reflect.Value, field reflect.StructField, key string, opt setOptions) (isSetted bool, err error) {
	if files := r.MultipartForm.File[key]; len(files) != 0 {
		return setByMultipartFormFile(value, field, files)
	}
	return setByForm(value, field, r.MultipartForm.Value, key, opt)
}

func setByMultipartFormFile(value reflect.Value, field reflect.StructField, files []*multipart.FileHeader) (isSetted bool, err error) {
	switch value.Kind() {
	case reflect.Ptr:
		switch value.Interface().(type) {
		case *multipart.FileHeader:
			value.Set(reflect.ValueOf(files[0]))
			return true, nil
		}
	case reflect.Struct:
		switch value.Interface().(type) {
		case multipart.FileHeader:
			value.Set(reflect.ValueOf(*files[0]))
			return true, nil
		}
	case reflect.Slice:
		slice := reflect.MakeSlice(value.Type(), len(files), len(files))
		isSetted, err = setArrayOfMultipartFormFiles(slice, field, files)
		if err != nil || !isSetted {
			return isSetted, err
		}
		value.Set(slice)
		return true, nil
	case reflect.Array:
		return setArrayOfMultipartFormFiles(value, field, files)
	}
	return false, errors.New("unsupported field type for multipart.FileHeader")
}

func setArrayOfMultipartFormFiles(value reflect.Value, field reflect.StructField, files []*multipart.FileHeader) (isSetted bool, err error) {
	if value.Len() != len(files) {
		return false, errors.New("unsupported len of array for []*multipart.FileHeader")
	}
	for i := range files {
		setted, err := setByMultipartFormFile(value.Index(i), field, files[i:i+1])
		if err != nil || !setted {
			return setted, err
		}
	}
	return true, nil
}

// setter tries to set value on a walking by fields of a struct
type setter interface {
	trySet(value reflect.Value, field reflect.StructField, key string, opt setOptions) (isSetted bool, err error)
}

type formSource map[string][]string

var (
	_ setter = formSource(nil)
    emptyField = reflect.StructField{}
)

// TrySet tries to set a value by request's form source (like map[string][]string)
func (form formSource) trySet(value reflect.Value, field reflect.StructField, tagValue string, opt setOptions) (isSetted bool, err error) {
	return setByForm(value, field, form, tagValue, opt)
}

func mappingByPtr(ptr interface{}, setter setter, tag string) error {
	_, err := mapping(reflect.ValueOf(ptr), emptyField, setter, tag)
	return err
}

func mapping(value reflect.Value, field reflect.StructField, setter setter, tag string) (bool, error) {
	if field.Tag.Get(tag) == "-" {
		return false, nil
	}
	var vKind = value.Kind()
	if vKind == reflect.Ptr {
		var isNew bool
		vPtr := value
		if value.IsNil() {
			isNew = true
			vPtr = reflect.New(value.Type().Elem())
		}
		isSetted, err := mapping(vPtr.Elem(), field, setter, tag)
		if err != nil {
			return false, err
		}
		if isNew && isSetted {
			value.Set(vPtr)
		}
		return isSetted, nil
	}

	if vKind != reflect.Struct || !field.Anonymous {
		ok, err := tryToSetValue(value, field, setter, tag)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}

	if vKind == reflect.Struct {
		tValue := value.Type()

		var isSetted bool
		for i := 0; i < value.NumField(); i++ {
			sf := tValue.Field(i)
			if sf.PkgPath != "" && !sf.Anonymous { // unexported
				continue
			}
			ok, err := mapping(value.Field(i), tValue.Field(i), setter, tag)
			if err != nil {
				return false, err
			}
			isSetted = isSetted || ok
		}
		return isSetted, nil
	}
	return false, nil
}

type setOptions struct {
	isDefaultExists bool
	defaultValue    string
}

func tryToSetValue(value reflect.Value, field reflect.StructField, setter setter, tag string) (bool, error) {
	var tagValue string
	var setOpt setOptions

	tagValue = field.Tag.Get(tag)
	tagValue, opts := head(tagValue, ",")

	if tagValue == "" { // default value is FieldName
		tagValue = field.Name
	}
	if tagValue == "" { // when field is "emptyField" variable
		return false, nil
	}

	var opt string
	for len(opts) > 0 {
		opt, opts = head(opts, ",")

		if k, v := head(opt, "="); k == "default" {
			setOpt.isDefaultExists = true
			setOpt.defaultValue = v
		}
	}
	return setter.trySet(value, field, tagValue, setOpt)
}

func setByForm(value reflect.Value, field reflect.StructField, form map[string][]string, tagValue string, opt setOptions) (isSetted bool, err error) {
	vs, ok := form[tagValue]
	if !ok && !opt.isDefaultExists {
		return false, nil
	}

	switch value.Kind() {
	case reflect.Slice:
		if !ok {
			vs = []string{opt.defaultValue}
		}
		return true, setSlice(vs, value, field)
	case reflect.Array:
		if !ok {
			vs = []string{opt.defaultValue}
		}
		if len(vs) != value.Len() {
			return false, fmt.Errorf("%q is not valid value for %s", vs, value.Type().String())
		}
		return true, setArray(vs, value, field)
	default:
		var val string
		if !ok {
			val = opt.defaultValue
		}

		if len(vs) > 0 {
			val = vs[0]
		}
		return true, setWithProperType(val, value, field)
	}
}

func setWithProperType(val string, value reflect.Value, field reflect.StructField) error {
	switch value.Kind() {
	case reflect.Int:
		return setIntField(val, 0, value)
	case reflect.Int8:
		return setIntField(val, 8, value)
	case reflect.Int16:
		return setIntField(val, 16, value)
	case reflect.Int32:
		return setIntField(val, 32, value)
	case reflect.Int64:
		switch value.Interface().(type) {
		case time.Duration:
			return setTimeDuration(val, value, field)
		}
		return setIntField(val, 64, value)
	case reflect.Uint:
		return setUintField(val, 0, value)
	case reflect.Uint8:
		return setUintField(val, 8, value)
	case reflect.Uint16:
		return setUintField(val, 16, value)
	case reflect.Uint32:
		return setUintField(val, 32, value)
	case reflect.Uint64:
		return setUintField(val, 64, value)
	case reflect.Bool:
		return setBoolField(val, value)
	case reflect.Float32:
		return setFloatField(val, 32, value)
	case reflect.Float64:
		return setFloatField(val, 64, value)
	case reflect.String:
		value.SetString(val)
	case reflect.Struct:
		switch value.Interface().(type) {
		case time.Time:
			return setTimeField(val, field, value)
		}
		return json.Unmarshal(stringToBytes(val), value.Addr().Interface())
	case reflect.Map:
		return json.Unmarshal(stringToBytes(val), value.Addr().Interface())
	default:
		return errors.New("unknown type")
	}
	return nil
}

func stringToBytes(s string) []byte {
	return *(*[]byte)(unsafe.Pointer(
		&struct {
			string
			Cap int
		}{s, len(s)},
	))
}

func setIntField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0"
	}
	intVal, err := strconv.ParseInt(val, 10, bitSize)
	if err == nil {
		field.SetInt(intVal)
	}
	return err
}

func setUintField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0"
	}
	uintVal, err := strconv.ParseUint(val, 10, bitSize)
	if err == nil {
		field.SetUint(uintVal)
	}
	return err
}

func setBoolField(val string, field reflect.Value) error {
	if val == "" {
		val = "false"
	}
	boolVal, err := strconv.ParseBool(val)
	if err == nil {
		field.SetBool(boolVal)
	}
	return err
}

func setFloatField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0.0"
	}
	floatVal, err := strconv.ParseFloat(val, bitSize)
	if err == nil {
		field.SetFloat(floatVal)
	}
	return err
}

func setTimeField(val string, structField reflect.StructField, value reflect.Value) error {
	timeFormat := structField.Tag.Get("time_format")
	if timeFormat == "" {
		timeFormat = time.RFC3339
	}

	switch tf := strings.ToLower(timeFormat); tf {
	case "unix", "unixnano":
		tv, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}

		d := time.Duration(1)
		if tf == "unixnano" {
			d = time.Second
		}

		t := time.Unix(tv/int64(d), tv%int64(d))
		value.Set(reflect.ValueOf(t))
		return nil

	}

	if val == "" {
		value.Set(reflect.ValueOf(time.Time{}))
		return nil
	}

	l := time.Local
	if isUTC, _ := strconv.ParseBool(structField.Tag.Get("time_utc")); isUTC {
		l = time.UTC
	}

	if locTag := structField.Tag.Get("time_location"); locTag != "" {
		loc, err := time.LoadLocation(locTag)
		if err != nil {
			return err
		}
		l = loc
	}

	t, err := time.ParseInLocation(timeFormat, val, l)
	if err != nil {
		return err
	}

	value.Set(reflect.ValueOf(t))
	return nil
}

func setArray(vals []string, value reflect.Value, field reflect.StructField) error {
	for i, s := range vals {
		err := setWithProperType(s, value.Index(i), field)
		if err != nil {
			return err
		}
	}
	return nil
}

func setSlice(vals []string, value reflect.Value, field reflect.StructField) error {
	slice := reflect.MakeSlice(value.Type(), len(vals), len(vals))
	err := setArray(vals, slice, field)
	if err != nil {
		return err
	}
	value.Set(slice)
	return nil
}
func setTimeDuration(val string, value reflect.Value, field reflect.StructField) error {
	d, err := time.ParseDuration(val)
	if err != nil {
		return err
	}
	value.Set(reflect.ValueOf(d))
	return nil
}

func head(str, sep string) (head string, tail string) {
	idx := strings.Index(str, sep)
	if idx < 0 {
		return str, ""
	}
	return str[:idx], str[idx+len(sep):]
}

func setFormMap(ptr interface{}, form map[string][]string) error {
	el := reflect.TypeOf(ptr).Elem()

	if el.Kind() == reflect.Slice {
		ptrMap, ok := ptr.(map[string][]string)
		if !ok {
			return errors.New("cannot convert to map slices of strings")
		}
		for k, v := range form {
			ptrMap[k] = v
		}
		return nil
	}
	ptrMap, ok := ptr.(map[string]string)
	if !ok {
		return errors.New("cannot convert to map of strings")
	}
	for k, v := range form {
		ptrMap[k] = v[len(v)-1] // pick last
	}
	return nil
}

type queryBinding struct{}

func (queryBinding) Name() string {
	return "query"
}

func (queryBinding) Bind(req *http.Request, obj interface{},rule string,f gvalid.RuleFunc) error {
	values := req.URL.Query()
	if err := mapFormByTag(obj, values,"form"); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

type uriBinding struct{}

func (uriBinding) Name() string {
	return "uri"
}

func (uriBinding) BindUri(m map[string][]string, obj interface{},rule string,f gvalid.RuleFunc) error {
	if err := mapFormByTag(obj, m,"uri"); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

type headerBinding struct{}

func (headerBinding) Name() string {
	return "header"
}

func (headerBinding) Bind(req *http.Request, obj interface{},rule string,f gvalid.RuleFunc) error {
	if err := mapHeader(obj, req.Header); err != nil {
		return err
	}
	var v gvalid.Validator
	if f != nil {
		return v.RuleFunc(rule,f).CheckStruct(obj)
	}
	return v.CheckStruct(obj)
}

func mapHeader(ptr interface{}, h map[string][]string) error {
	return mappingByPtr(ptr, headerSource(h), "header")
}

type headerSource map[string][]string

var _ setter = headerSource(nil)

func (hs headerSource) trySet(value reflect.Value, field reflect.StructField, tagValue string, opt setOptions) (isSetted bool, err error) {
	return setByForm(value, field, hs, textproto.CanonicalMIMEHeaderKey(tagValue), opt)
}
//
//type yamlBinding struct{}
//
//func (yamlBinding) Name() string {
//	return "yaml"
//}

//func (yamlBinding) Bind(req *http.Request, obj interface{}) error {
//	return decodeYAML(req.Body, obj)
//}
//
//func (yamlBinding) BindBody(body []byte, obj interface{}) error {
//	return decodeYAML(bytes.NewReader(body), obj)
//}
//
//func decodeYAML(r io.Reader, obj interface{}) error {
//	decoder := yaml.NewDecoder(r)
//	if err := decoder.Decode(obj); err != nil {
//		return err
//	}
//	return validate(obj)
//}


//type protobufBinding struct{}
//
//func (protobufBinding) Name() string {
//	return "protobuf"
//}

//func (b protobufBinding) Bind(req *http.Request, obj interface{}) error {
//	buf, err := ioutil.ReadAll(req.Body)
//	if err != nil {
//		return err
//	}
//	return b.BindBody(buf, obj)
//}
//
//func (protobufBinding) BindBody(body []byte, obj interface{}) error {
//	if err := proto.Unmarshal(body, obj.(proto.Message)); err != nil {
//		return err
//	}
//	return nil
//}

//type msgpackBinding struct{}
//
//func (msgpackBinding) Name() string {
//	return "msgpack"
//}

//func (msgpackBinding) Bind(req *http.Request, obj interface{}) error {
//	return decodeMsgPack(req.Body, obj)
//}
//
//func (msgpackBinding) BindBody(body []byte, obj interface{}) error {
//	return decodeMsgPack(bytes.NewReader(body), obj)
//}
//
//func decodeMsgPack(r io.Reader, obj interface{}) error {
//	cdc := new(codec.MsgpackHandle)
//	if err := codec.NewDecoder(r, cdc).Decode(&obj); err != nil {
//		return err
//	}
//	return validate(obj)
//}
