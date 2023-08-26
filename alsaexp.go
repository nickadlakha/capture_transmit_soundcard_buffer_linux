package main

/*
#cgo LDFLAGS: -lsctp
#include <stdio.h>
#include <stdlib.h>
#include <strings.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <netinet/sctp.h>
#include <unistd.h>

#define SIZE 1024
#define SPORT 3000

unsigned char buf[SIZE];
struct sctp_sndrcvinfo sinfo;
int sockfd, slen, flags;
struct sockaddr_in serv_addr, client_addr;
struct sctp_event_subscribe events;

int server(void) {
	int client_sockfd;

    sockfd = socket(AF_INET, SOCK_SEQPACKET, IPPROTO_SCTP);
    if (sockfd < 0) {
        perror("Error creating sctp socket");
        exit(20);
    }

    bzero(&serv_addr, sizeof(serv_addr));
    serv_addr.sin_family = AF_INET;
    serv_addr.sin_addr.s_addr = htonl(INADDR_ANY);
    serv_addr.sin_port = htons(SPORT);
    if (bind(sockfd, (struct sockaddr *) &serv_addr, sizeof(serv_addr)) < 0) {
        perror("Address binding failed");
        exit(21);
    }

	bzero(&events, sizeof(events));
    events.sctp_association_event = 1;

	if (setsockopt(sockfd, IPPROTO_SCTP, SCTP_EVENTS, &events, sizeof(events))) {
        perror("set sock opt\n");
    }

    listen(sockfd, SOMAXCONN);
    printf("Listening on sctp server port %d\n", SPORT);
    slen = sizeof(client_addr);
    sctp_recvmsg(sockfd, buf, SIZE, (struct sockaddr *) &client_addr, &slen, &sinfo,
    		&flags);

	bzero(&sinfo, sizeof(sinfo));
    sinfo.sinfo_flags |= SCTP_SENDALL;
	return sockfd;
}

void wait_for_client() {
	sctp_recvmsg(sockfd, buf, SIZE, (struct sockaddr *) &client_addr, &slen, &sinfo,
		&flags);
}

int client(char *addr) {
	sockfd = socket(AF_INET, SOCK_STREAM, IPPROTO_SCTP);

    if (sockfd < 0) {
        perror("Error creating sctp socket");
        exit(22);
    }

    bzero(&serv_addr, sizeof(serv_addr));
    serv_addr.sin_family = AF_INET;
    serv_addr.sin_addr.s_addr = inet_addr(addr);
    serv_addr.sin_port = htons(SPORT);

	if (connect(sockfd, (struct sockaddr *)&serv_addr, sizeof(serv_addr)) < 0) {
    		perror("connect to server failed");
        exit(23);
    }

    printf("Connected to server port %d\n", SPORT);
    return sockfd;
}
*/
import "C"

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"unsafe"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

type cstate int

const (
	EMP cstate = iota
	SR
	CL
)

var cs cstate = EMP

type socketWrRd C.int

func (s socketWrRd) Write(p []byte) (int, error) {
	n := C.sctp_send(C.int(s), unsafe.Pointer(&p[0]), C.ulong(len(p)), &C.sinfo, 0)

	if n <= 0 {
		return 0, fmt.Errorf("sctp send error")
	}

	return int(n), nil
}

func (s socketWrRd) Read(p []byte) (int, error) {
	var slen C.uint
	n := C.sctp_recvmsg(C.int(s), unsafe.Pointer(&p[0]), 64*C.SIZE,
		(*C.struct_sockaddr)(unsafe.Pointer(&C.serv_addr)), &slen, &C.sinfo, &C.flags)

	if n <= 0 {
		return 0, fmt.Errorf("sctp read error")
	}

	return int(n), nil
}

func prompt() {
	fmt.Fprintf(os.Stderr, "\nPress enter [s] to transmit, [c] to consume and enter to exit.....::")
}

func start_transmitter(finish <-chan bool) {
	c, err := pulse.NewClient()

	if err != nil {
		log.Panic(err)
	}

	defer c.Close()

	s, err := c.DefaultSink()

	if err != nil {
		log.Panic(err)
	}

	iopr, iopw := io.Pipe()
	defer iopr.Close()
	defer iopw.Close()

	stream, err := c.NewRecord(pulse.NewWriter(iopw, proto.FormatInt16LE), pulse.RecordMonitor(s), pulse.RecordSampleRate(44100), pulse.RecordStereo)

	if err != nil {
		log.Panic(err)
	}

	defer stream.Close()
	defer stream.Stop()

	var sw socketWrRd = socketWrRd(C.server())
	defer C.close(C.int(sw))
	ifinish := make(chan struct{})

	stream.Start()

	go func() {
		buf := make([]byte, 64*1024)

		for {
			select {
			case <-ifinish:
				return
			default:
				io.CopyBuffer(sw, iopr, buf)
				//log.Println(err)
				C.wait_for_client()
			}
		}
	}()

	log.Println("Starting transmission")
	prompt()
	<-finish
	close(ifinish)
}

func start_capture(finish chan bool, latency int, server string) {
	iopr, iopw := io.Pipe()
	defer iopw.Close()

	var swr socketWrRd = socketWrRd(C.client(C.CString(server)))
	c, err := pulse.NewClient()
	if err != nil {
		log.Panic(err)
	}

	stream, err := c.NewPlayback(pulse.NewReader(iopr, proto.FormatInt16LE), pulse.PlaybackSampleRate(44100), pulse.PlaybackStereo, pulse.PlaybackLatency(float64(latency)))
	if err != nil {
		log.Panic(err)
	}

	go func(st *pulse.PlaybackStream) {
		defer iopr.Close()
		defer C.close(C.int(swr))
		defer c.Close()
		defer stream.Close()

		go func() {
			st.Start()
			st.Drain()
		}()

		buf := make([]byte, 64*1024)
		_, err := io.CopyBuffer(iopw, swr, buf)
		log.Println(err, st.Error(), st.Underflow(), st.Running())

		if err.Error() == "sctp read error" {
			cs = EMP
			finish <- true
		}
		prompt()
		return
	}(stream)

	prompt()
	<-finish
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	bread := bufio.NewReader(os.Stdin)
	var (
		latency int = 1
		input   []byte
		err     error
	)

	if len(os.Args) == 2 {
		latency, _ = strconv.Atoi(os.Args[1])
	}

	finish := make(chan bool)
	defer close(finish)
	prompt()

	for {
		input, err = bread.ReadBytes('\n')

		if err != nil {
			continue
		}

		if input[0] == '\n' {
			break
		} else if input[0] == 's' {
			if cs == EMP || cs == CL {
				if cs == CL {
					finish <- true
				}

				go start_transmitter(finish)
				cs = SR
			} else {
				prompt()
			}
		} else if input[0] == 'c' {
			if cs == EMP || cs == SR {
				if cs == SR {
					finish <- true
				}

			GETSERVERADDRESS:
				fmt.Fprintf(os.Stderr, "\nPlease enter the server address to connect.....::")
				input, err = bread.ReadBytes('\n')

				if err != nil {
					fmt.Fprintf(os.Stderr, "\nError, please re-enter the server address")
					goto GETSERVERADDRESS
				}
				go start_capture(finish, latency, string(input))
				cs = CL
			} else {
				prompt()
			}
		}
	}
}
