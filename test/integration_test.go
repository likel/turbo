package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/vaporz/turbo"
	"github.com/vaporz/turbo/test/testservice/gen"
	"github.com/vaporz/turbo/test/testservice/gen/proto"
	tgen "github.com/vaporz/turbo/test/testservice/gen/thrift/gen-go/gen"
	gcomponent "github.com/vaporz/turbo/test/testservice/grpcapi/component"
	gimpl "github.com/vaporz/turbo/test/testservice/grpcservice/impl"
	tcompoent "github.com/vaporz/turbo/test/testservice/thriftapi/component"
	timpl "github.com/vaporz/turbo/test/testservice/thriftservice/impl"
	"github.com/vaporz/turbo/turbo/cmd"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"text/template"
	"time"
)

func TestMain(m *testing.M) {
	os.RemoveAll(turbo.GOPATH() + "/src/github.com/vaporz/turbo/test/testcreateservice")
	os.Exit(m.Run())
}

func TestCreateGrpcService(t *testing.T) {
	create(t, "grpc")
	generate(t, "grpc")
	overwriteProto()
	os.RemoveAll(turbo.GOPATH() + "/src/github.com/vaporz/turbo/test/testcreateservice/gen")
	generate(t, "grpc")
}

func TestCreateThriftService(t *testing.T) {
	create(t, "thrift")
	generate(t, "thrift")
	overwriteThrift()
	os.RemoveAll(turbo.GOPATH() + "/src/github.com/vaporz/turbo/test/testcreateservice/gen")
	generate(t, "thrift")
	// recover grpc gen code
	generate(t, "grpc")
}

func TestGrpcService(t *testing.T) {
	httpPort := "8081"
	overwriteServiceYaml("8081", "50051", "development")

	s := turbo.NewGrpcServer("testservice/service.yaml")
	s.Initializer = &testInitializer{}
	go s.StartGRPC(gcomponent.GrpcClient, gen.GrpcSwitcher, gimpl.RegisterServer)
	time.Sleep(time.Millisecond * 500)

	runCommonTests(t, s.Server, httpPort, "grpc")

	testGet(t, "http://localhost:"+httpPort+"/hello/error",
		"rpc error: code = Unknown desc = grpc error\n")

	s.Components.WithErrorHandler(s.Component("errorHandler").(turbo.ErrorHandlerFunc))
	testGet(t, "http://localhost:"+httpPort+"/hello/error",
		"from errorHandler:rpc error: code = Unknown desc = grpc error")
	s.Components.Reset()

	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("ContextValueInterceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"test1_intercepted:{\"message\":\"{\\\"values\\\":{},\\\"yourName\\\":\\\"testtest\\\",\\\"int64Value\\\":1234567,\\\"boolValue\\\":true,\\\"float64Value\\\":1.23,\\\"uint64Value\\\":456}\"}")
	s.Components.Reset()

	testGet(t, "http://localhost:"+httpPort+"/hello/testtest?int64_value=64&bool_value=true&float64_value=0.123&uint64_value=123",
		"{\"message\":\"{\\\"values\\\":{},\\\"yourName\\\":\\\"testtest\\\",\\\"int64Value\\\":64,\\\"boolValue\\\":true,\\\"float64Value\\\":0.123,\\\"uint64Value\\\":123}\"}")

	s.Components.SetMessageFieldConvertor("CommonValues", s.Component("convertProtoCommonValues").(turbo.Convertor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest?bool_value=true",
		"{\"message\":\"{\\\"values\\\":{\\\"someId\\\":1111111},\\\"yourName\\\":\\\"testtest\\\",\\\"boolValue\\\":true}\"}")
	s.Components.Reset()

	body := strings.NewReader("{\"values\":{\"someId\":123}, \"yourName\":\"a name\", \"boolValue\":true}")
	testPostWithContentType(t, "http://localhost:"+httpPort+"/hello", "application/json", body,
		"{\"message\":\"{\\\"values\\\":{\\\"someId\\\":123},\\\"yourName\\\":\\\"a name\\\",\\\"boolValue\\\":true}\"}")

	s.Stop()
	time.Sleep(time.Millisecond * 100)
}

func TestThriftService(t *testing.T) {
	httpPort := "8082"
	overwriteServiceYaml(httpPort, "50052", "production")

	s := turbo.NewThriftServer("testservice/service.yaml")
	s.Initializer = &testInitializer{}
	go s.StartTHRIFT(tcompoent.ThriftClient, gen.ThriftSwitcher, timpl.TProcessor)
	time.Sleep(time.Second * 2)
	turbo.SetOutput(os.Stdout)

	runCommonTests(t, s.Server, httpPort, "thrift")

	testGet(t, "http://localhost:"+httpPort+"/hello/error",
		"Internal error processing sayHello: thrift error\n")

	s.Components.WithErrorHandler(s.Component("errorHandler").(turbo.ErrorHandlerFunc))
	testGet(t, "http://localhost:"+httpPort+"/hello/error",
		"from errorHandler:Internal error processing sayHello: thrift error")
	s.Components.Reset()

	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("ContextValueInterceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"test1_intercepted:{\"message\":\"[thrift server]values.TransactionId=0, yourName=testtest,int64Value=1234567, boolValue=true, float64Value=1.230000, uint64Value=456, int32Value=0, int16Value=0\"}")
	s.Components.Reset()

	testGet(t, "http://localhost:"+httpPort+"/hello/testtest?transaction_id=111&int64_value=64&bool_value=true&float64_value=0.123&uint64_value=123&int32_value=32&int16_value=16",
		"{\"message\":\"[thrift server]values.TransactionId=111, yourName=testtest,int64Value=64, boolValue=true, float64Value=0.123000, uint64Value=123, int32Value=32, int16Value=16\"}")

	s.Components.SetMessageFieldConvertor("CommonValues", s.Component("convertThriftCommonValues").(turbo.Convertor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest?bool_value=true",
		"{\"message\":\"[thrift server]values.TransactionId=222222, yourName=testtest,int64Value=0, boolValue=true, float64Value=0.000000, uint64Value=0, int32Value=0, int16Value=0\"}")
	s.Components.Reset()

	body := strings.NewReader("{\"StringValue\":\"123\", \"int32Value\":456, \"boolvalue\":true}")
	testPostWithContentType(t, "http://localhost:"+httpPort+"/testjson", "application/json", body,
		"{\"message\":\"[thrift server]json= TestJsonRequest({StringValue:123 Int32Value:456 BoolValue:true})\"}")

	body = strings.NewReader("{\"BoolValue\":true}")
	testPostWithContentType(t, "http://localhost:"+httpPort+"/testjson/123/456", "application/json", body,
		"{\"message\":\"[thrift server]json= TestJsonRequest({StringValue:123 Int32Value:456 BoolValue:true})\"}")

	s.Stop()
	time.Sleep(time.Millisecond * 100)
}

func TestHTTPGrpcService(t *testing.T) {
	httpPort := "8083"
	overwriteServiceYaml(httpPort, "50053", "development")

	s := turbo.NewGrpcServer("testservice/service.yaml")
	go s.StartGrpcService(gimpl.RegisterServer)
	time.Sleep(time.Millisecond * 300)

	go s.StartGrpcHTTPServer(gcomponent.GrpcClient, gen.GrpcSwitcher)
	time.Sleep(time.Millisecond * 300)

	testGet(t, "http://localhost:"+httpPort+"/hello/testtest", "{\"message\":\"[grpc server]Hello, testtest\"}")

	s.Stop()
	time.Sleep(time.Millisecond * 100)
}

func TestHTTPThriftService(t *testing.T) {
	httpPort := "8084"
	overwriteServiceYaml(httpPort, "50054", "development")

	s := turbo.NewThriftServer("testservice/service.yaml")
	go s.StartThriftService(timpl.TProcessor)
	time.Sleep(time.Millisecond * 500)

	go s.StartThriftHTTPServer(tcompoent.ThriftClient, gen.ThriftSwitcher)
	time.Sleep(time.Millisecond * 500)

	testGet(t, "http://localhost:"+httpPort+"/hello/testtest", "{\"message\":\"[thrift server]Hello, testtest\"}")

	s.Stop()
	time.Sleep(time.Millisecond * 100)
}

func TestLoadComponentsFromConfig(t *testing.T) {
	httpPort := "8085"
	overwriteServiceYamlWithGrpcComponents(httpPort, "50055", "production")

	s := turbo.NewGrpcServer("testservice/service.yaml")
	assert.Nil(t, s.Component("test"))
	s.Initializer = &testInitializer{}
	go s.StartGrpcService(gimpl.RegisterServer)
	time.Sleep(time.Millisecond * 300)

	go s.StartGrpcHTTPServer(gcomponent.GrpcClient, gen.GrpcSwitcher)
	time.Sleep(time.Millisecond * 300)
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest", "{\"message\":\"[grpc server]Hello, testtest\"}")
	testGet(t, "http://localhost:"+httpPort+"/hello", "intercepted:{\"message\":\"[grpc server]Hello, \"}")
	testGet(t, "http://localhost:"+httpPort+"/hellointerceptor", "interceptor_error:from errorHandler:error!")
	testGet(t, "http://localhost:"+httpPort+"/hello_preprocessor", "preprocessor:{\"message\":\"[grpc server]Hello, \"}")
	testGet(t, "http://localhost:"+httpPort+"/hello_postprocessor", "postprocessor:[grpc server]Hello, ")
	testGet(t, "http://localhost:"+httpPort+"/hello_hijacker", "hijacker")
	testGet(t, "http://localhost:"+httpPort+"/hello_convertor?bool_value=true", "{\"message\":\"{\\\"values\\\":{\\\"someId\\\":1111111},\\\"boolValue\\\":true}\"}")
	testGet(t, "http://localhost:"+httpPort+"/hello_hijacker", "hijacker")
	testGet(t, "http://localhost:"+httpPort+"/hello/error", "from errorHandler:rpc error: code = Unknown desc = grpc error")

	changeServiceYamlWithGrpcComponents(httpPort, "50055", "production")
	time.Sleep(time.Millisecond * 100)
	testGet(t, "http://localhost:"+httpPort+"/hello", "test1_intercepted:preprocessor:postprocessor:[grpc server]Hello, ")
	s.Stop()
	time.Sleep(time.Millisecond * 100)
}

func overwriteProto() {
	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testcreateservice/testcreateservice.proto",
		`syntax = "proto3";
import "shared.proto";
package proto;

message SayHelloRequest {
    CommonValues values = 1;
    string yourName = 2;
    int64 int64Value = 3;
    bool boolValue = 4;
    double float64Value = 5;
    uint64 uint64Value = 6;
}

message SayHelloResponse {
    string message = 1;
}

service TestCreateService {
    rpc sayHello (SayHelloRequest) returns (SayHelloResponse) {}
}
`,
		nil,
	)
	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testcreateservice/shared.proto",
		`syntax = "proto3";
package proto;

message CommonValues {
    int64 someId = 1;
}
`,
		nil,
	)
}

func overwriteThrift() {
	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testcreateservice/shared.thrift",
		`namespace go gen

struct CommonValues {
  1: i64 transactionId,
}

struct HelloValues {
  1: string message,
}
`,
		nil,
	)
	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testcreateservice/testcreateservice.thrift",
		`namespace go gen
include "shared.thrift"

struct SayHelloResponse {
  1: string message,
}

service TestCreateService {
    SayHelloResponse sayHello (1:shared.CommonValues values, 2:string yourName, 3:i64 int64Value, 4:bool boolValue, 5:double float64Value, 6:i64 uint64Value)
}
`,
		nil,
	)

	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testcreateservice/thriftservice/impl/testcreateserviceimpl.go",
		`package impl

import (
	"github.com/vaporz/turbo/test/testcreateservice/gen/thrift/gen-go/gen"
	"git.apache.org/thrift.git/lib/go/thrift"
)

func TProcessor() thrift.TProcessor {
	return gen.NewTestCreateServiceProcessor(TestCreateService{})
}

type TestCreateService struct {
}

func (s TestCreateService) SayHello(values *gen.CommonValues, yourName string, int64Value int64, boolValue bool, float64Value float64, uint64Value int64) (r *gen.SayHelloResponse, err error) {
	return &gen.SayHelloResponse{Message: "[thrift server]Hello, " + yourName}, nil
}
`,
		nil,
	)

}

func create(t *testing.T, rpc string) {
	cmd.RootCmd.SetArgs([]string{"create", "github.com/vaporz/turbo/test/testcreateservice"})
	err := cmd.Execute()
	assert.Equal(t, "invalid args", err.Error())

	cmd.RootCmd.SetArgs([]string{"create", "github.com/vaporz/turbo/test/testcreateservice", "test_create_service"})
	err = cmd.Execute()
	assert.Contains(t, err.Error(), "not a CamelCase string")

	cmd.RootCmd.SetArgs([]string{"create", "github.com/vaporz/turbo/test/testcreateservice", "TestCreateService", "-r", "aaa"})
	err = cmd.Execute()
	assert.Contains(t, err.Error(), "invalid value for -r, should be grpc or thrift")

	cmd.RootCmd.SetArgs([]string{"create", "github.com/vaporz/turbo/test/testcreateservice", "TestCreateService", "-r", rpc, "-f", "true"})
	err = cmd.Execute()
	assert.Nil(t, err)
	cmd.RpcType = ""
	cmd.FilePaths = []string{}
}

func generate(t *testing.T, rpc string) {
	cmd.RootCmd.SetArgs([]string{"generate"})
	err := cmd.Execute()
	assert.Equal(t, "Usage: generate [package_path]", err.Error())

	cmd.RootCmd.SetArgs([]string{"generate", "github.com/vaporz/turbo/test/testcreateservice"})
	err = cmd.Execute()
	assert.Equal(t, "missing rpctype (-r)", err.Error())

	cmd.RootCmd.SetArgs([]string{"generate", "github.com/vaporz/turbo/test/testcreateservice", "-r", "unknown"})
	err = cmd.Execute()
	assert.Equal(t, "invalid rpctype", err.Error())

	if rpc == "grpc" {
		cmd.RootCmd.SetArgs([]string{"generate", "github.com/vaporz/turbo/test/testcreateservice", "-r", rpc})
		err = cmd.Execute()
		assert.Equal(t, "missing .proto file path (-I)", err.Error())
	}

	cmd.RootCmd.SetArgs([]string{"generate", "github.com/vaporz/turbo/test/testcreateservice", "-r", rpc,
		"-I", turbo.GOPATH() + "/src/github.com/vaporz/turbo/test/testcreateservice"})
	err = cmd.Execute()
	assert.Nil(t, err)

	cmd.RpcType = ""
	cmd.FilePaths = []string{}
}

func runCommonTests(t *testing.T, s *turbo.Server, httpPort, rpcType string) {
	testGet(t, "http://localhost:"+httpPort+"/hello",
		"{\"message\":\"["+rpcType+" server]Hello, \"}")
	testGet(t, "http://localhost:"+httpPort+"/hello?your_name=turbo",
		"{\"message\":\"["+rpcType+" server]Hello, turbo\"}")
	testGet(t, "http://localhost:"+httpPort+"/hello?your_name=turbo&yourname=xxx",
		"{\"message\":\"["+rpcType+" server]Hello, xxx\"}")
	testGet(t, "http://localhost:"+httpPort+"/hello/vaporz?yourName=turbo&yourname=xxx",
		"{\"message\":\"["+rpcType+" server]Hello, xxx\"}")
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"{\"message\":\"["+rpcType+" server]Hello, testtest\"}")
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest?your_name=aaa",
		"{\"message\":\"["+rpcType+" server]Hello, testtest\"}")
	testPost(t, "http://localhost:"+httpPort+"/hello/testtest",
		"404 page not found\n")

	s.Components.SetCommonInterceptor(s.Component("Test1Interceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"test1_intercepted:{\"message\":\"["+rpcType+" server]Hello, testtest\"}")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/", s.Component("TestInterceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest?yourName=testname",
		"intercepted:{\"message\":\"["+rpcType+" server]Hello, testname\"}")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/", s.Component("TestInterceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:{\"message\":\"["+rpcType+" server]Hello, testtest\"}")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("BeforeErrorInterceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"interceptor_error:error!\n")

	s.Components.Reset()
	list := turbo.Interceptors{s.Component("BaseInterceptor").(turbo.Interceptor), s.Component("BeforeErrorInterceptor").(turbo.Interceptor)}
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", list...)
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"interceptor_error:error!\n")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("TestInterceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:{\"message\":\"["+rpcType+" server]Hello, testtest\"}")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("TestInterceptor").(turbo.Interceptor), s.Component("Test1Interceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:test1_intercepted:{\"message\":\"["+rpcType+" server]Hello, testtest\"}")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("TestInterceptor").(turbo.Interceptor), s.Component("AfterErrorInterceptor").(turbo.Interceptor), s.Component("Test1Interceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:test1_intercepted:{\"message\":\"["+rpcType+" server]Hello, testtest\"}:after_error:")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("TestInterceptor").(turbo.Interceptor), s.Component("BeforeErrorInterceptor").(turbo.Interceptor), s.Component("Test1Interceptor").(turbo.Interceptor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:interceptor_error:error!\n")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("TestInterceptor").(turbo.Interceptor))
	s.Components.SetPreprocessor([]string{}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("errorPreProcessor").(turbo.Preprocessor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:error_preprocessor:error in preprocessor\n")

	s.Components.Reset()
	s.Components.Intercept([]string{"GET"}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("TestInterceptor").(turbo.Interceptor))
	s.Components.SetPreprocessor([]string{}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("preProcessor").(turbo.Preprocessor))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:preprocessor:{\"message\":\"["+rpcType+" server]Hello, testtest\"}")

	if rpcType == "thrift" {
		s.Components.SetPostprocessor([]string{}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("thriftPostProcessor").(turbo.Postprocessor))
	} else {
		s.Components.SetPostprocessor([]string{}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("postProcessor").(turbo.Postprocessor))
	}
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:preprocessor:postprocessor:["+rpcType+" server]Hello, testtest")

	s.Components.SetHijacker([]string{}, "/hello/{your_name:[a-zA-Z0-9]+}", s.Component("hijacker").(turbo.Hijacker))
	testGet(t, "http://localhost:"+httpPort+"/hello/testtest",
		"intercepted:hijacker")
	s.Components.Reset()
}

func testPostWithContentType(t *testing.T, url, contentType string, body io.Reader, expected string) {
	resp, err := http.Post(url, contentType, body)
	if err != nil {
		t.Fail()
	}
	defer resp.Body.Close()
	assert.Nil(t, err)
	assert.Equal(t, expected, readResp(resp))
}

func testPost(t *testing.T, url, expected string) {
	testPostWithContentType(t, url, "", nil, expected)
}

func readResp(resp *http.Response) string {
	var bytes bytes.Buffer
	bytes.ReadFrom(resp.Body)
	return bytes.String()
}

func testGet(t *testing.T, url, expected string) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fail()
	}
	defer resp.Body.Close()
	assert.Nil(t, err)
	assert.Equal(t, expected, readResp(resp))
}

type testInitializer struct {
}

func (t *testInitializer) InitService(s *turbo.Server) error {
	s.RegisterComponent("BaseInterceptor", &turbo.BaseInterceptor{})
	s.RegisterComponent("BeforeErrorInterceptor", &BeforeErrorInterceptor{})
	s.RegisterComponent("AfterErrorInterceptor", &AfterErrorInterceptor{})
	s.RegisterComponent("TestInterceptor", &TestInterceptor{})
	s.RegisterComponent("Test1Interceptor", &Test1Interceptor{})
	s.RegisterComponent("ContextValueInterceptor", &ContextValueInterceptor{})
	s.RegisterComponent("preProcessor", preProcessor)
	s.RegisterComponent("errorPreProcessor", errorPreProcessor)
	s.RegisterComponent("postProcessor", postProcessor)
	s.RegisterComponent("thriftPostProcessor", thriftPostProcessor)
	s.RegisterComponent("hijacker", hijacker)
	s.RegisterComponent("errorHandler", errorHandler)
	s.RegisterComponent("convertProtoCommonValues", convertProtoCommonValues)
	s.RegisterComponent("convertThriftCommonValues", convertThriftCommonValues)
	return nil
}

func (t *testInitializer) StopService(s *turbo.Server) {
}

type BeforeErrorInterceptor struct {
	turbo.BaseInterceptor
}

func (l *BeforeErrorInterceptor) Before(resp http.ResponseWriter, req *http.Request) (*http.Request, error) {
	resp.Write([]byte("interceptor_error:"))
	return req, errors.New("error!")
}

type AfterErrorInterceptor struct {
	turbo.BaseInterceptor
}

func (l *AfterErrorInterceptor) After(resp http.ResponseWriter, req *http.Request) (*http.Request, error) {
	fmt.Println("[After] Request URL:" + req.URL.Path)
	resp.Write([]byte(":after_error:"))
	return req, errors.New("error: after interceptor")
}

type TestInterceptor struct {
	turbo.BaseInterceptor
}

func (l *TestInterceptor) Before(resp http.ResponseWriter, req *http.Request) (*http.Request, error) {
	fmt.Println("TestInterceptor before")
	resp.Write([]byte("intercepted:"))
	return req, nil
}

func (l *TestInterceptor) After(resp http.ResponseWriter, req *http.Request) (*http.Request, error) {
	fmt.Println("[After] Request URL:" + req.URL.Path)
	return req, nil
}

type Test1Interceptor struct {
	turbo.BaseInterceptor
}

func (l *Test1Interceptor) Before(resp http.ResponseWriter, req *http.Request) (*http.Request, error) {
	resp.Write([]byte("test1_intercepted:"))
	return req, nil
}

func (l *Test1Interceptor) After(resp http.ResponseWriter, req *http.Request) (*http.Request, error) {
	fmt.Println("[After] Request URL:" + req.URL.Path)
	return req, nil
}

type ContextValueInterceptor struct {
	turbo.BaseInterceptor
}

func (l *ContextValueInterceptor) Before(resp http.ResponseWriter, req *http.Request) (*http.Request, error) {
	ctx := req.Context()
	fmt.Println("set context!!")
	ctx = context.WithValue(ctx, "bool_value", "true")
	ctx = context.WithValue(ctx, "Int64Value", "1234567")
	ctx = context.WithValue(ctx, "float64_value", "1.23")
	ctx = context.WithValue(ctx, "uint64value", "456")
	resp.Write([]byte("test1_intercepted:"))
	return req.WithContext(ctx), nil
}

var preProcessor turbo.Preprocessor = func(resp http.ResponseWriter, req *http.Request) error {
	resp.Write([]byte("preprocessor:"))
	return nil
}

var errorPreProcessor turbo.Preprocessor = func(resp http.ResponseWriter, req *http.Request) error {
	resp.Write([]byte("error_preprocessor:"))
	return errors.New("error in preprocessor")
}

var postProcessor turbo.Postprocessor = func(resp http.ResponseWriter, req *http.Request, serviceResp interface{}, err error) {
	r := serviceResp.(*proto.SayHelloResponse)
	resp.Write([]byte("postprocessor:" + r.Message))
}

var thriftPostProcessor turbo.Postprocessor = func(resp http.ResponseWriter, req *http.Request, serviceResp interface{}, err error) {
	r := serviceResp.(*tgen.SayHelloResponse)
	resp.Write([]byte("postprocessor:" + r.Message))
}

var hijacker turbo.Hijacker = func(resp http.ResponseWriter, req *http.Request) {
	resp.Write([]byte("hijacker"))
}

var errorHandler turbo.ErrorHandlerFunc = func(resp http.ResponseWriter, req *http.Request, err error) {
	resp.Write([]byte("from errorHandler:" + err.Error()))
}

var convertProtoCommonValues turbo.Convertor = func(req *http.Request) reflect.Value {
	result := &proto.CommonValues{}
	result.SomeId = 1111111
	return reflect.ValueOf(result)
}

var convertThriftCommonValues turbo.Convertor = func(req *http.Request) reflect.Value {
	result := &tgen.CommonValues{}
	result.TransactionId = 222222
	return reflect.ValueOf(result)
}

func overwriteServiceYaml(httpPort, servicePort, env string) {
	type serviceYamlValues struct {
		HttpPort    string
		ServiceName string
		ServicePort string
		Env         string
	}
	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testservice/service.yaml",
		`config:
  http_port: {{.HttpPort}}
  environment: {{.Env}}
  turbo_log_path: log
  grpc_service_name: {{.ServiceName}}
  grpc_service_host: 127.0.0.1
  grpc_service_port: {{.ServicePort}}
  thrift_service_name: {{.ServiceName}}
  thrift_service_host: 127.0.0.1
  thrift_service_port: {{.ServicePort}}

urlmapping:
  - GET /hello/{your_Name:[a-zA-Z0-9]+} SayHello
  - GET,POST /hello SayHello
  - POST /testjson TestJson
  - POST /testjson/{StringValue:[a-zA-Z0-9]+}/{int32_value:[a-zA-Z0-9]+} TestJson
`,
		serviceYamlValues{
			HttpPort:    httpPort,
			ServiceName: "TestService",
			ServicePort: servicePort,
			Env:         env,
		},
	)
}

func overwriteServiceYamlWithGrpcComponents(httpPort, servicePort, env string) {
	type serviceYamlValues struct {
		HttpPort        string
		ServiceName     string
		ServicePort     string
		Env             string
		ServiceRootPath string
	}
	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testservice/service.yaml",
		`config:
  http_port: {{.HttpPort}}
  environment: {{.Env}}
  turbo_log_path:
  service_root_path: {{.ServiceRootPath}}
  grpc_service_name: {{.ServiceName}}
  grpc_service_host: 127.0.0.1
  grpc_service_port: {{.ServicePort}}
  thrift_service_name: {{.ServiceName}}
  thrift_service_host: 127.0.0.1
  thrift_service_port: {{.ServicePort}}

urlmapping:
  - GET /hello/{your_Name:[a-zA-Z0-9]+} SayHello
  - GET /hello SayHello
  - GET /hellointerceptor SayHello
  - GET /hello_preprocessor SayHello
  - GET /hello_postprocessor SayHello
  - GET /hello_hijacker SayHello
  - GET /hello_convertor SayHello
  - POST /testjson TestJson
  - POST /testjson/{StringValue:[a-zA-Z0-9]+}/{int32_value:[a-zA-Z0-9]+} TestJson

interceptor:
  - GET /hello TestInterceptor
  - GET /hellointerceptor BeforeErrorInterceptor,Test1Interceptor
preprocessor:
  - GET /hello_preprocessor preProcessor
postprocessor:
  - GET /hello_postprocessor postProcessor
hijacker:
  - GET /hello_hijacker hijacker
convertor:
  - CommonValues convertProtoCommonValues
errorhandler: errorHandler
`,
		serviceYamlValues{
			HttpPort:        httpPort,
			ServiceName:     "TestService",
			ServicePort:     servicePort,
			Env:             env,
			ServiceRootPath: "github.com/vaporz/turbo/test/testservice",
		},
	)
}

func changeServiceYamlWithGrpcComponents(httpPort, servicePort, env string) {
	type serviceYamlValues struct {
		HttpPort    string
		ServiceName string
		ServicePort string
		Env         string
	}
	writeFileWithTemplate(
		turbo.GOPATH()+"/src/github.com/vaporz/turbo/test/testservice/service.yaml",
		`config:
  http_port: {{.HttpPort}}
  environment: {{.Env}}
  turbo_log_path: log
  grpc_service_name: {{.ServiceName}}
  grpc_service_host: 127.0.0.1
  grpc_service_port: {{.ServicePort}}
  thrift_service_name: {{.ServiceName}}
  thrift_service_host: 127.0.0.1
  thrift_service_port: {{.ServicePort}}

urlmapping:
  - GET /hello/{your_Name:[a-zA-Z0-9]+} SayHello
  - GET /hello SayHello
  - GET /hellointerceptor SayHello
  - GET /hello_preprocessor SayHello
  - GET /hello_postprocessor SayHello
  - GET /hello_hijacker SayHello
  - GET /hello_convertor SayHello
  - POST /testjson TestJson
  - POST /testjson/{StringValue:[a-zA-Z0-9]+}/{int32_value:[a-zA-Z0-9]+} TestJson

interceptor:
  - GET /hello Test1Interceptor
preprocessor:
  - GET /hello preProcessor
postprocessor:
  - GET /hello postProcessor
`,
		serviceYamlValues{
			HttpPort:    httpPort,
			ServiceName: "TestService",
			ServicePort: servicePort,
			Env:         env,
		},
	)
}

func writeFileWithTemplate(filePath, text string, data interface{}) {
	f, err := os.Create(filePath)
	if err != nil {
		panic("fail to create file:" + filePath)
	}
	tmpl, err := template.New("").Parse(text)
	if err != nil {
		panic(err)
	}
	err = tmpl.Execute(f, data)
	if err != nil {
		panic(err)
	}
}
