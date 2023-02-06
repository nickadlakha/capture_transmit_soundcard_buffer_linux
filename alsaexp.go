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
    sctp_recvmsg(sockfd, buf, SIZE, (struct sockaddr *) &client_addr, &slen,
    		&sinfo, &flags);

	bzero(&sinfo, sizeof(sinfo));
    sinfo.sinfo_flags |= SCTP_SENDALL;
	return sockfd;
}

void wait_for_client() {
	sctp_recvmsg(sockfd, buf, SIZE, (struct sockaddr *) &client_addr, &slen,
    		&sinfo, &flags);
}

int client(char *addr) {
	sockfd = socket(AF_INET, SOCK_SEQPACKET, IPPROTO_SCTP);
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
	"fmt"
	"os"

	"io"

	"strconv"
	"unsafe"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

type socketWrRd C.int

func (s socketWrRd) Write(p []byte) (int, error) {
	n := C.sctp_send(C.int(s), unsafe.Pointer(&p[0]), C.ulong(len(p)), &C.sinfo, 0)

	if n < 0 {
		return 0, fmt.Errorf("sctp send error")
	}

	return int(n), nil
}

func (s socketWrRd) Read(p []byte) (int, error) {
	var slen C.uint
	n := C.sctp_recvmsg(C.int(s), unsafe.Pointer(&p[0]), 32*C.SIZE,
		(*C.struct_sockaddr)(unsafe.Pointer(&C.serv_addr)), &slen, &C.sinfo, &C.flags)

	if n < 0 {
		return 0, fmt.Errorf("sctp read error")
	}

	return int(n), nil
}

func start_transmitter(finish chan struct{}) {
	c, err := pulse.NewClient()

	if err != nil {
		panic(err)
	}

	defer c.Close()

	s, err := c.DefaultSink()

	if err != nil {
		panic(err)
	}

	iopr, iopw := io.Pipe()
	defer iopr.Close()
	defer iopw.Close()

	stream, err := c.NewRecord(pulse.NewWriter(iopw, proto.FormatInt16LE), pulse.RecordMonitor(s), pulse.RecordSampleRate(44100), pulse.RecordStereo)

	if err != nil {
		panic(err)
	}

	defer stream.Close()
	defer stream.Stop()

	var sw socketWrRd = socketWrRd(C.server())
	defer C.close(C.int(sw))
	stream.Start()

	go func() {
	REPEAT:
		_, err := io.Copy(sw, iopr)

		if err != nil {
			fmt.Println(err)
			C.wait_for_client()
			goto REPEAT
		}
	}()

	fmt.Println("Staring transmission")
	<-finish
}

func start_capture(stdin bool, latency int) {
	iopr, iopw := io.Pipe()
	defer iopr.Close()
	defer iopw.Close()

	if stdin {
		go io.Copy(iopw, os.Stdin)
	} else {
		if len(os.Args) <= 1 {
			fmt.Fprintf(os.Stderr, "Please provide the server address to connect\n")
			return
		}

		var swr socketWrRd = socketWrRd(C.client(C.CString(os.Args[1])))
		defer C.close(C.int(swr))

		go func() {
			_, err := io.Copy(iopw, swr)
			fmt.Println("Server closed the connection ", err)
			os.Exit(24)
		}()
	}

	c, err := pulse.NewClient()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.Close()

	stream, err := c.NewPlayback(pulse.NewReader(iopr, proto.FormatInt16LE), pulse.PlaybackSampleRate(44100), pulse.PlaybackStereo, pulse.PlaybackLatency(float64(latency)))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer stream.Close()
	stream.Start()
	stream.Drain()
}

func main() {
	latency := 1

	if len(os.Args) == 3 {
		latency, _ = strconv.Atoi(os.Args[2])
	}

	if len(os.Args) > 1 && os.Args[1] == "-" {
		start_capture(true, latency)
	}

	fmt.Print("Press enter [s] to transmit, [c] to consume and enter to exit.....\n")
	input := make([]byte, 2)

	finish := make(chan struct{})

	for {
		os.Stdin.Read(input)

		if input[0] == '\n' {
			close(finish)
			break
		} else if input[0] == 's' {
			go start_transmitter(finish)
		} else if input[0] == 'c' {
			start_capture(false, latency)
		}
	}

}
