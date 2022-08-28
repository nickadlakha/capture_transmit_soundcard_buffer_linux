package main

import (
	"fmt"
	"net"
	"os"

	"io"

	"strconv"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

const (
	address         = "239.0.0.0:9999"
	maxDatagramSize = 8 * 1024
)

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

	addr, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		panic(err)
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		panic(err)
	}

	conn.SetWriteBuffer(maxDatagramSize)

	iopr, iopw := io.Pipe()

	stream, err := c.NewRecord(pulse.NewWriter(iopw, proto.FormatInt16LE), pulse.RecordMonitor(s), pulse.RecordSampleRate(44100), pulse.RecordStereo)

	if err != nil {
		panic(err)
	}

	stream.Start()

	go io.Copy(conn, iopr)

	fmt.Println("Staring transmission")
	<-finish
	stream.Stop()
}

func start_capture(stdin bool, latency int) {
	iopr, iopw := io.Pipe()

	if stdin {
		go io.Copy(iopw, os.Stdin)
	} else {
		if len(os.Args) <= 1 {
			fmt.Fprintf(os.Stderr, "Please provide the server address to connect\n")
			return
		}

		addr, err := net.ResolveUDPAddr("udp4", address)
		if err != nil {
			panic(err)
		}

		conn, err := net.ListenMulticastUDP("udp4", nil, addr)
		if err != nil {
			panic(err)
		}

		conn.SetReadBuffer(maxDatagramSize)

		go io.Copy(iopw, conn)
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
	stream.Start()
	stream.Drain()

	if stream.Underflow() {
		for {
			stream.Stop()
			stream.Start()
		}
	}

	stream.Close()
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
