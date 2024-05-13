## Pre-requisites ##
* `sudo apt-get install -y libsctp-dev`

## Compiling ##
* `go mod tidy`
* `go build alsaexp.go`

## Running ##
**As Server** 
```
xxxx@yyy:~$ ./alsaexp

Press enter [s] to transmit, [c] to consume and enter to exit.....::s
Listening on sctp server port 3000
```

**As Client**
```
xxxx@yyy:~$ ./alsaexp

Press enter [s] to transmit, [c] to consume and enter to exit.....::c

Please enter the server address to connect.....::192.168.1.229
```
