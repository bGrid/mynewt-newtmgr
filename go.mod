module github.com/bgrid/mynewt-newtmgr

go 1.12

require (
	github.com/JuulLabs-OSS/cbgo v0.0.2
	github.com/abiosoft/readline v0.0.0-20180607040430-155bce2042db // indirect
	github.com/fatih/color v1.9.0 // indirect
	github.com/fatih/structs v1.1.0
	github.com/flynn-archive/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/joaojeronimo/go-crc16 v0.0.0-20140729130949-59bd0194935e
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/pkg/errors v0.9.1
	github.com/rigado/ble v0.5.12
	github.com/runtimeco/go-coap v0.0.0-20190911184520-8e5532820fc0
	github.com/sirupsen/logrus v1.6.0
	github.com/spf13/cast v1.3.1
	github.com/spf13/cobra v1.1.2
	github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07
	github.com/ugorji/go/codec v1.1.8
	golang.org/x/net v0.0.0-20200602114024-627f9648deb9
	gopkg.in/abiosoft/ishell.v2 v2.0.0
	gopkg.in/cheggaaa/pb.v1 v1.0.28
	mynewt.apache.org/newt v0.0.0-20200612205247-d7efc36caf73
	mynewt.apache.org/newtmgr v0.0.0-20200603005556-61395d2a7056
)

replace mynewt.apache.org/newtmgr => ./
