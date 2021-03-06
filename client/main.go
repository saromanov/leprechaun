package client

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/kilgaloon/leprechaun/agent"

	"github.com/fsnotify/fsnotify"
	"github.com/kilgaloon/leprechaun/api"
	"github.com/kilgaloon/leprechaun/config"
)

// Agent holds instance of Client
var Agent *Client

// Client settings and configurations
type Client struct {
	*agent.Default
	stopped bool
	Queue
}

// New create client
// Creating new agent will enable usage of Agent variable globally for packages
// that use this package
func New(name string, cfg *config.AgentConfig) *Client {
	client := &Client{
		agent.New(name, cfg),
		false,
		Queue{},
	}

	Agent = client

	return client
}

// Start client
func (client *Client) Start() {
	// if client is stopped/paused, just unpause it
	client.GetMutex().Lock()
	if client.stopped {
		client.stopped = false
		return
	}
	client.GetMutex().Unlock()
	// remove hanging .lock file
	os.Remove(client.GetConfig().GetLockFile())
	// SetPID of client
	client.SetPID()
	// build queue
	client.Lock()
	client.BuildQueue()
	client.Unlock()

	// watch for new recipes
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic("Failed to create watcher")
	}

	defer watcher.Close()
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Create == fsnotify.Create {
					client.AddToQueue(&client.Queue.Stack, event.Name)
				}
			case err := <-watcher.Errors:
				client.Error("error: %s", err)
			}
		}
	}()

	err = watcher.Add(client.GetConfig().GetRecipesPath())
	if err != nil {
		fmt.Println(err)
	}

	// register client to command socket
	go api.New(client.GetConfig().GetCommandSocket()).Register(client)
	// dispatch event that client is ready
	client.Event.Dispatch("client:ready")

	for {
		go client.ProcessQueue()
		time.Sleep(60 * time.Second)
	}

}

// RegisterCommands to be used in socket communication
// If you want to takeover default commands from agent
// call DefaultCommands from Agent which is same command
func (client *Client) RegisterCommands() map[string]api.Command {
	cmds := make(map[string]api.Command)

	cmds["info"] = api.Command{
		Closure: client.clientInfo,
		Definition: api.Definition{
			Text:  "Display some basic info about running client",
			Usage: "client info",
		},
	}

	cmds["stop"] = api.Command{
		Closure: client.Stop,
		Definition: api.Definition{
			Text:  "Display some basic info about running client",
			Usage: "client info",
		},
	}

	// this function merge both maps and inject default commands from agent
	return client.DefaultCommands(cmds)
}

// SetPID sets current PID of client
func (client *Client) SetPID() {
	client.GetMutex().Lock()
	defer client.GetMutex().Unlock()

	f, err := os.OpenFile(client.GetConfig().GetPIDFile(), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		panic("Failed to start client, can't save PID, reason: " + err.Error())
	}

	client.PID = os.Getpid()
	pid := strconv.Itoa(client.PID)
	_, err = f.WriteString(pid)
	if err != nil {
		panic("Failed to start client, can't save PID")
	}
}

// GetPID gets current PID of client
func (client *Client) GetPID() int {
	data, err := ioutil.ReadFile(client.GetConfig().GetPIDFile())
	if err != nil {
		panic("Failed to get PID")
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		panic("Failed to get PID")
	}

	return pid
}

// Check does client is working on something
// decide this in which status client resides
func (client *Client) isWorking() bool {
	// check does .lock file exists
	if _, err := os.Stat(client.GetConfig().GetLockFile()); os.IsNotExist(err) {
		return false
	}

	return true
}

// Lock client to busy state
func (client *Client) Lock() {
	_, err := os.OpenFile(client.GetConfig().GetLockFile(), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		panic("Failed to lock client in busy state")
	}
}

// Unlock client to busy state
func (client *Client) Unlock() {
	os.Remove(client.GetConfig().GetLockFile())
}

// Stop client
func (client *Client) Stop(r io.Writer, args ...string) ([][]string, error) {
	client.GetMutex().Lock()
	defer client.GetMutex().Unlock()

	var resp [][]string

	client.stopped = true
	resp = [][]string{
		{"Schedule client stopped!"},
	}

	return resp, nil
}
