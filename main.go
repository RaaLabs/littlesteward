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
	scriptFile string
	hostsFile  string

	statusFile string
	statusCh   chan nodeStatus
	doneFile   string
	doneCh     chan nodeDone

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
		scriptFile: scriptFile,
		hostsFile:  "hosts.txt",

		statusFile: "status.log",
		statusCh:   make(chan nodeStatus),
		doneFile:   "done.log",
		doneCh:     make(chan nodeDone),

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
type nodeDone struct {
	node node
	done chan struct{}
}

// nodeStatus, used for signaling that we want to write to the status.log file.
type nodeStatus struct {
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

				s.statusCh <- nodeStatus{
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
	nd := nodeDone{
		node: n,
		done: doneNode,
	}

	// Check if we are able to contact node.
	_, err := net.DialTimeout("tcp", n.ip+":22", time.Second*5)
	if err != nil {
		return fmt.Errorf("error: unable to reach node: %v", err)
	}

	// TODO: do the actual ssh command here.
	sshTimeout := 30

	// Copy the script file using scp to the node.
	scpCmd := fmt.Sprintf("scp -rp -o ConnectTimeout=%v -o StrictHostKeyChecking=no -i %v %v %v@%v:", sshTimeout, s.idRSAFile, s.scriptFile, s.sshUser, n.ip)
	out, err := exec.Command("/bin/bash", "-c", scpCmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error: failed to execute scp command: %v", err)
	}

	// Write to the status.log file.
	{
		done := make(chan struct{}, 1)

		s.statusCh <- nodeStatus{
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

	sshScript := fmt.Sprintf("sudo bash -c 'export NODENAME=%v; /home/%v/%v'", n.name, s.sshUser, s.scriptFile)
	sshCmd := fmt.Sprintf("ssh -o ConnectTimeout=%v -n -i %v %v@%v \"%v\"", sshTimeout, s.idRSAFile, s.sshUser, n.ip, sshScript)
	// fmt.Printf(" * sshCmd : %v\n", sshCmd)

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

	// Write to the status.log file.
	{
		done := make(chan struct{}, 1)

		s.statusCh <- nodeStatus{
			node: n,
			text: "info: script ok: " + string(out),
			done: done,
		}
		// log.Printf("%v\n", err)
		<-done
	}

	// -----------------------

	// Signal that we are done with the current node.
	s.doneCh <- nd
	<-doneNode

	return nil
}

// handleDone will handle the done.log file.
func (s *server) doneHandler(ctx context.Context) error {
	fhDone, err := os.OpenFile(s.doneFile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0755)
	if err != nil {
		return fmt.Errorf("error: opening done file: %v", err)
	}
	defer fhDone.Close()

	for {
		select {
		case nd := <-s.doneCh:
			_, err := fhDone.Write([]byte(nd.node.ip + "," + nd.node.name + "\n"))
			nd.done <- struct{}{}
			if err != nil {
				return fmt.Errorf("error: writing to done file: %v", err)
			}

			err = s.removeNodeFromFile(nd.node)
			if err != nil {
				return err
			}

		case <-ctx.Done():
			fmt.Println(" * exiting handleDone")
			return nil
		}
	}
}

// Will handle the writing to the status.log file.
func (s *server) statusHandler(ctx context.Context) error {
	fh, err := os.OpenFile(s.statusFile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0755)
	if err != nil {
		return fmt.Errorf("error: opening status file: %v", err)
	}
	defer fh.Close()

	for {
		select {
		case st := <-s.statusCh:
			_, err := fh.Write([]byte(st.node.ip + "," + st.node.name + "," + st.text + "\n"))
			st.done <- struct{}{}
			if err != nil {
				return fmt.Errorf("error: writing to done file: %v", err)
			}

		case <-ctx.Done():
			fmt.Println(" * exiting statusHandler")
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

// Remove the given node argument from hosts.txt file.
func (s *server) removeNodeFromFile(n node) error {
	fh, err := os.Open(s.hostsFile)
	if err != nil {
		return fmt.Errorf("error: unable to open hosts file: %v", err)
	}

	scanner := bufio.NewScanner(fh)
	nodes := []node{}

	// Read one line at a time from the hostfile, and append
	// all the nodes, except the one given as argument.
	for scanner.Scan() {
		sp := strings.Split(scanner.Text(), ",")
		tmpN := node{
			ip:   sp[0],
			name: sp[1],
		}

		if tmpN.ip != n.ip {
			nodes = append(nodes, tmpN)
		}
	}
	err = fh.Close()
	if err != nil {
		return fmt.Errorf("error: unable to close hosts file: %v", err)
	}

	// Write the new node slice, where the one to remove is removed
	// back to the hosts file.
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

	return nil
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
