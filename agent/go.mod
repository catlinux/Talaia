module github.com/catlinux/Talaia/agent

go 1.26.0

require (
	connectrpc.com/connect/v2 v2.0.1
	github.com/catlinux/Talaia/proto v0.0.0-00010101000000-000000000000
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/shirou/gopsutil/v4 v4.26.9
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/power-devops/perfstat v0.0.0-20260805114148-88456608a4f6 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

replace github.com/catlinux/Talaia/proto => ../proto
