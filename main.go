package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var wg sync.WaitGroup

// server holds general variables for the service.
type server struct {
	// The script to execute on the host.
	scriptFile string
	// The file who contains all the hosts to run the script on.
	hostsFile string
	// Channel to signal to remove a node entry from the hosts file.
	hostsRemoveCh chan nodeEvent

	// The overall status log file.
	failedFile   string
	failedFileCh chan nodeEvent
	doneFile     string
	doneFileCh   chan nodeEvent

	sshUser   string
	idRSAFile string
}

// newServer returns a new *server
func newServer(scriptFile string, sshUser string, idRSAFile string) (*server, error) {
	if sshUser == "" {
		return nil, fmt.Errorf("error: sshUser flag connot be empty")
	}
	if idRSAFile == "" {
		return nil, fmt.Errorf("error: idRSAFile flag connot be empty")
	}
	if scriptFile == "" {
		return nil, fmt.Errorf("error: script flag cannot be empty")
	}

	s := server{
		scriptFile:    scriptFile,
		hostsFile:     "hosts.txt",
		hostsRemoveCh: make(chan nodeEvent),

		failedFile:   "failed.log",
		failedFileCh: make(chan nodeEvent),
		doneFile:     "done.log",
		doneFileCh:   make(chan nodeEvent),

		sshUser:   sshUser,
		idRSAFile: idRSAFile,
	}

	return &s, nil
}

// node details
type node struct {
	ip   string
	name string
}

// nodeDone, used when signaling that we are done processing a node.
type nodeEvent struct {
	node node
	text string
	done chan struct{}
}

// run holds all the logic for starting and stopping all handlers.
func (s *server) run() error {
	nodes, err := s.getNodesFromFile()
	if err != nil {
		return err
	}

	if len(nodes) < 1 {
		return io.EOF
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wgFile sync.WaitGroup

	// Start the handling of the removal og nodes from hosts file.
	wgFile.Add(1)
	go func() {
		err := s.hostsHandler(ctx)
		if err != nil {
			log.Printf("%v\n", err)
			cancel()
			os.Exit(1)
		}
		wgFile.Done()
	}()

	// Start the handling of the done.log file
	wgFile.Add(1)
	go func() {
		err := s.doneHandler(ctx)
		if err != nil {
			log.Printf("%v\n", err)
			cancel()
			os.Exit(1)
		}
		wgFile.Done()
	}()

	// Start the handling of the status.log file
	wgFile.Add(1)
	go func() {
		err := s.statusHandler(ctx)
		if err != nil {
			log.Printf("%v\n", err)
			cancel()
			os.Exit(1)
		}
		wgFile.Done()
	}()

	// Range over all the nodes, and start a handler for each node.
	for _, n := range nodes {
		wg.Add(1)
		go func(n node) {
			err := s.handleNode(ctx, n)
			if err != nil {
				// Write to the status.log file.
				done := make(chan struct{}, 1)

				s.failedFileCh <- nodeEvent{
					node: n,
					text: err.Error(),
					done: done,
				}
				log.Printf("%v\n", err)
				<-done
			}
			wg.Done()
		}(n)
	}

	// Wait for all handle nodes to get done.
	wg.Wait()

	// cancel all file handling.
	cancel()

	// Wait for all file handling to get done.
	wgFile.Wait()

	return nil
}

// handleHost handles each individual host entry read from the hosts file.
func (s *server) handleNode(ctx context.Context, n node) error {
	doneNode := make(chan struct{}, 1)
	nd := nodeEvent{
		node: n,
		done: doneNode,
	}

	// Check if we are able to contact node.
	log.Printf("%v,%v: trying to connect\n", n.ip, n.name)
	_, err := net.DialTimeout("tcp", n.ip+":22", time.Second*5)
	if err != nil {
		return fmt.Errorf("error: unable to reach node: %v", err)
	}
	log.Printf("%v,%v: got ack for connection\n", n.ip, n.name)

	sshTimeout := 30

	// Copy the script file using scp to the node.
	log.Printf("%v,%v: trying to copy script\n", n.ip, n.name)

	scpCmd := fmt.Sprintf("scp -rp -o ConnectTimeout=%v -o StrictHostKeyChecking=no -i %v %v %v@%v:", sshTimeout, s.idRSAFile, s.scriptFile, s.sshUser, n.ip)
	out, err := exec.Command("/bin/bash", "-c", scpCmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error: failed to execute scp command: %v", err)
	}
	log.Printf("%v,%v: script copied\n", n.ip, n.name)

	// Write to the status.log file.
	{
		done := make(chan struct{}, 1)

		s.failedFileCh <- nodeEvent{
			node: n,
			text: "successfully copied the script to the node : " + string(out),
			done: done,
		}
		// log.Printf("%v\n", err)
		<-done
	}

	// ssh to the node, and exexute the script
	// ssh -o ConnectTimeout=$sshTimeout -n -i $idRsaFile "$sshUser"@"$ipAddress" "sudo bash -c 'export NODENAME=$name; $scpDestinationFolder/$scriptName'" 2>&1

	// -----------------------

	log.Printf("%v,%v: trying to execute script\n", n.ip, n.name)

	sshScript := fmt.Sprintf("sudo bash -c 'export NODENAME=%v; /home/%v/%v'", n.name, s.sshUser, s.scriptFile)
	sshCmd := fmt.Sprintf("ssh -o ConnectTimeout=%v -n -i %v %v@%v \"%v\"", sshTimeout, s.idRSAFile, s.sshUser, n.ip, sshScript)

	cmd := exec.Command("/bin/bash", "-c", sshCmd)

	// use StdoutPipe so we can get the whole output message, and not just the exit code.
	pipe, _ := cmd.StdoutPipe()
	outSlice := []string{}
	go func() {
		buf := bufio.NewScanner(pipe)
		for buf.Scan() {
			log.Printf("%v\n", buf.Text())
			outSlice = append(outSlice, buf.Text())

		}
	}()

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("error: cmd.Start failed:%v", err)
	}

	err = cmd.Wait()
	out = []byte(strings.Join(outSlice, ","))
	if err != nil {
		return fmt.Errorf("error: ssh cmd failed: %v: %v", err, string(out))
	}

	log.Printf("%v,%v: script executed\n", n.ip, n.name)

	// Write to the status.log file.
	{
		done := make(chan struct{}, 1)

		s.failedFileCh <- nodeEvent{
			node: n,
			text: "info: script ok: " + string(out),
			done: done,
		}
		// log.Printf("%v\n", err)
		<-done
	}

	// -----------------------

	// Signal that we are done with the current node.
	nd.text = string(out)
	s.doneFileCh <- nd
	<-doneNode

	return nil
}

// handleDone will handle the done.log file and also initiate a removal
// of the the node from the hosts file when the node is done.
func (s *server) doneHandler(ctx context.Context) error {
	fhDone, err := os.OpenFile(s.doneFile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0755)
	if err != nil {
		return fmt.Errorf("error: opening done file: %v", err)
	}
	defer fhDone.Close()

	for {
		select {
		case nd := <-s.doneFileCh:
			_, err := fhDone.Write([]byte(nd.node.ip + "," + nd.node.name + "," + nd.text + "\n"))
			nd.done <- struct{}{}
			if err != nil {
				return fmt.Errorf("error: writing to done file: %v", err)
			}

			// Remove the node from the hosts file.
			ndHostsRemove := nd
			ndHostsRemove.done = make(chan struct{})
			s.hostsRemoveCh <- ndHostsRemove
			<-ndHostsRemove.done

		case <-ctx.Done():
			log.Println(" * exiting handleDone")
			return nil
		}
	}
}

// Will handle the writing to the status.log file.
func (s *server) statusHandler(ctx context.Context) error {
	fh, err := os.OpenFile(s.failedFile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0755)
	if err != nil {
		return fmt.Errorf("error: opening status file: %v", err)
	}
	defer fh.Close()

	for {
		select {
		case st := <-s.failedFileCh:
			_, err := fh.Write([]byte(st.node.ip + "," + st.node.name + "," + st.text + "\n"))
			st.done <- struct{}{}
			if err != nil {
				return fmt.Errorf("error: writing to done file: %v", err)
			}

		case <-ctx.Done():
			log.Println(" * exiting statusHandler")
			return nil
		}
	}
}

// Get all the nodes from hosts.txt file.
func (s *server) getNodesFromFile() ([]node, error) {
	fhHosts, err := os.Open(s.hostsFile)
	if err != nil {
		return nil, fmt.Errorf("error: unable to open hosts file: %v", err)
	}
	defer fhHosts.Close()

	scanner := bufio.NewScanner(fhHosts)
	nodes := []node{}

	// Read one line at a time from the hostfile, and do the handler.
	for scanner.Scan() {
		sp := strings.Split(scanner.Text(), ",")
		n := node{
			ip:   sp[0],
			name: sp[1],
		}
		nodes = append(nodes, n)
	}

	return nodes, nil
}

// Removes a node from hosts.txt file. Received the node to remove
// on the s.hostsRemoveNodeCh.
func (s *server) hostsHandler(ctx context.Context) error {
	for {
		select {
		case n := <-s.hostsRemoveCh:

			// Open and read all the current nodes from the hosts.txt file.
			// Put all the nodes into the nodes slice, except the one to
			// remove.
			fh, err := os.Open(s.hostsFile)
			if err != nil {
				return fmt.Errorf("error: unable to open hosts file: %v", err)
			}

			scanner := bufio.NewScanner(fh)
			nodes := []node{}

			for scanner.Scan() {
				sp := strings.Split(scanner.Text(), ",")
				tmpN := node{
					ip:   sp[0],
					name: sp[1],
				}

				if tmpN.ip != n.node.ip {
					nodes = append(nodes, tmpN)
				}
			}
			err = fh.Close()
			if err != nil {
				return fmt.Errorf("error: unable to close hosts file: %v", err)
			}

			// Write the new node slice back to the hosts file,
			// where the one to remove is removed
			fh, err = os.OpenFile(s.hostsFile, os.O_TRUNC|os.O_RDWR, 0755)
			if err != nil {
				return fmt.Errorf("error: unable to open hosts file for writing new: %v", err)
			}

			for _, v := range nodes {
				_, err := fh.Write([]byte(v.ip + "," + v.name + "\n"))
				if err != nil {
					return fmt.Errorf("error: unable to write node to hosts file: %v", err)
				}
			}

			err = fh.Close()
			if err != nil {
				return fmt.Errorf("error: unable to close hosts file: %v", err)
			}

			n.done <- struct{}{}

		case <-ctx.Done():
			log.Println(" * exiting removeNodeHostsFile")
			return nil
		}
	}
}

func main() {
	script := flag.String("script", "", "the script to exexute")
	sshUser := flag.String("sshUser", "", "ssh user id")
	idRSAFile := flag.String("idRSAFile", "", "the id rsa file to use for ssh authentication")
	flag.Parse()

	s, err := newServer(*script, *sshUser, *idRSAFile)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}

	for {
		// Loop until we get and EOF.
		err := s.run()
		if err != nil && err != io.EOF {
			log.Printf("%v\n", err)
			os.Exit(1)
		}
		if err == io.EOF {
			log.Printf("info: all nodes done: %v\n", err)
			return
		}

		// putting in a little timer so we don't spam the nodes with reconnects.
		time.Sleep(time.Second * 5)
	}

}
