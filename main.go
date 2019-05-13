package main

import (
	"log"
	"io"
	"time"
	"net"
	"fmt"
	"flag"
	"regexp"
	"github.com/sevlyar/go-daemon"
)




type ByteRead struct {
	byteread []byte
	bread    int
	err      error
}

func readNonBlock(incoming io.Reader, chunkchan chan ByteRead, keepalive *bool) {
	for *keepalive {
		chunk := make([]byte, 1024)
		bread, err := incoming.Read(chunk)
		chunkchan <- ByteRead{chunk, bread, err}
	}
}

func crosstalk(incoming net.Conn, forward string) {
	defer incoming.Close()
	outgoing, _ := net.Dial("tcp", forward)
	defer outgoing.Close()

	var err error
	var errout error

	chunkchanupload := make(chan ByteRead)
	chunkchandownload := make(chan ByteRead)
	keepalive := true
	go readNonBlock(incoming, chunkchanupload, &keepalive)
	go readNonBlock(outgoing, chunkchandownload, &keepalive)

	for err == nil && errout == nil {
		select {
		case br := <-chunkchanupload:
			if br.err == nil {
				_, errout = outgoing.Write(br.byteread[:br.bread])
				//log.Println(convname)
			} else {
				err = br.err
			}
		case br := <-chunkchandownload:
			if br.err == nil {
				_, errout = incoming.Write(br.byteread[:br.bread])
				//log.Println(convname)
			} else {
				err = br.err
			}
		}
	}
	keepalive = false
	log.Println("Closing connection because of errors")
	log.Println(err)
	log.Println(errout)
}

type AcceptConn struct {
	Conn net.Conn
	Err error
}
func listenAndFwdChan(accepted chan AcceptConn,ln net.Listener) {
	for {
		c,e := ln.Accept()	
		accepted <- AcceptConn{c,e}
	}
}

func die(alive chan bool,autokillsec int) {
	<-time.After(time.Duration(autokillsec) * time.Second)
	alive <- true
}

// To terminate the daemon use:
//  kill `cat pid`

func main() {
	var local = flag.String("local", ":10001", "local bind")
	var remote = flag.String("remote", "127.0.0.1:3389", "remote bind")
	var namestart = flag.String("name", "pfwd-10001","pidname for forwarding")
	var clientaddr = flag.String("clientaddr", "127.0.0.1","client address for connecting")
	var piddir = flag.String("piddir","/tmp","directory for storing pid");
	var logdir = flag.String("logdir","/tmp","directory for storing log");
	var workdir = flag.String("workdir","/tmp","directory for working");
	var autokill = flag.Int("autokill",(60*15),"kill the daemon automatically after x seconds")
	flag.Parse()

	cntxt := &daemon.Context{
		PidFileName: fmt.Sprintf("%s/%s.pid",*piddir,*namestart),
		PidFilePerm: 0644,
		LogFileName: fmt.Sprintf("%s/%s.log",*logdir,*namestart),
		LogFilePerm: 0640,
		WorkDir:     *workdir,
		Umask:       027,
		Args:        []string{*namestart,"-local",*local,"-remote",*remote,"-name",*namestart,"-clientaddr",*clientaddr,"-autokill",fmt.Sprintf("%d",*autokill)},
	}

	d, err := cntxt.Reborn()
	if err != nil {
		log.Fatal("Unable to run: ", err)
	}
	if d != nil {
		return
	}
	defer cntxt.Release()

	rev4 := regexp.MustCompile(`^([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}):[0-9]{1,5}$`)

	log.Print("- - - - - - - - - - - - - - -")
	log.Print("daemon started")




	ln, err := net.Listen("tcp", *local)
	if err != nil {
		log.Print("Error ...")
		log.Print(err)
	} else {
		alive := make(chan bool)
		go die(alive,*autokill)

		acceptconn := make(chan AcceptConn)
		go listenAndFwdChan(acceptconn,ln)

		log.Printf("Listening on %s",*local)
		log.Printf("Forwarding to %s",*remote)
		
		keepalive := true

		for keepalive {
			select {
				case <-alive:
					log.Print("Should die now")
					keepalive = false
				case ac:= <-acceptconn:
					conn := ac.Conn
					err := ac.Err
					if err != nil {
						// handle error
						log.Print("Error ...")
						log.Print(err)
					} else {
						client := conn.RemoteAddr()
						clientstr := client.String()
						

						allowed := false
						testv4 := rev4.FindStringSubmatch(clientstr)

						if len(testv4) > 1 && testv4[1] == *clientaddr {
							allowed = true
						}

						if allowed {
							log.Printf("Incoming and opening %s for client %s",*remote,clientstr)
							go crosstalk(conn, *remote)
						} else {
							chunk := make([]byte, 2048)
							bread, _ := conn.Read(chunk)
							if bread > 0 {
								t := time.Now()
								log.Print("Unauthorized request",client.String())
								fmt.Fprintf(conn,`

HTTP/1.0 200
Content-Type: text/html; charset=UTF-8
Referrer-Policy: no-referrer
Content-Length: 13
Date: %s

It's working!
`,t.String)
							}
							conn.Close()
						}
					}
			}

		}
	}
}

