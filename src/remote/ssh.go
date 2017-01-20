package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"time"

	"github.com/lfkeitel/saturn/src/utils"

	"golang.org/x/crypto/ssh"
)

var sshClientConfig *ssh.ClientConfig

func LoadPrivateKey(config *utils.Config) error {
	if config.SSH.Username == "" {
		return errors.New("No SSH username configured")
	}

	authMethods := make([]ssh.AuthMethod, 0, 2)

	if config.SSH.PrivateKey != "" {
		sshPrivateKey, err := ioutil.ReadFile(config.SSH.PrivateKey)
		if err != nil {
			return err
		}

		signer, err := ssh.ParsePrivateKey(sshPrivateKey)
		if err != nil {
			return err
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if config.SSH.Password != "" {
		authMethods = append(authMethods, ssh.Password(config.SSH.Password))
	}

	if len(authMethods) == 0 {
		return errors.New("No SSH authentication methods configured")
	}

	t, _ := time.ParseDuration(config.SSH.Timeout)

	sshClientConfig = &ssh.ClientConfig{
		User:    config.SSH.Username,
		Auth:    authMethods,
		Timeout: t,
	}
	return nil
}

func UploadScript(config *utils.Config, hosts map[string]*utils.ConfigHost, genFilename string) error {
	f, err := os.Open(genFilename)
	if err != nil {
		return err
	}
	defer f.Close()

	s, err := f.Stat()
	if err != nil {
		return err
	}

	for _, host := range hosts {
		if host.Disable {
			continue
		}

		_, err = f.Seek(0, 0) // rewind file reader
		if err != nil {
			return err
		}

		if err := uploadRemoteScript(config, host, f, s); err != nil {
			log.Println(err.Error())
			host.Disable = true
		}
	}

	return nil
}

func uploadRemoteScript(config *utils.Config, host *utils.ConfigHost, f *os.File, s os.FileInfo) error {
	if host.SSHConnection == nil {
		if err := host.ConnectSSH(sshClientConfig); err != nil {
			return err
		}
	}

	session, err := host.SSHConnection.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintln(w, "D0755", 0, ".saturn") // mkdir
		fmt.Fprintf(w, "C%#o %d %s\n", s.Mode().Perm(), s.Size(), path.Base(f.Name()))
		io.Copy(w, f)
		fmt.Fprint(w, "\x00")
	}()

	cmd := fmt.Sprintf("scp -rt %s", config.Core.RemoteBaseDir)
	return session.Run(cmd)
}

func ExecuteScript(config *utils.Config, hosts map[string]*utils.ConfigHost, filename string) ([]*utils.HostResponse, error) {
	filename = path.Base(filename)
	responses := make([]*utils.HostResponse, 0, len(hosts))
	for _, host := range hosts {
		if host.Disable {
			continue
		}

		if host.SSHConnection == nil {
			if err := host.ConnectSSH(sshClientConfig); err != nil {
				return nil, err
			}
		}

		session, err := host.SSHConnection.NewSession()
		if err != nil {
			return nil, err
		}

		var stdoutBuf bytes.Buffer
		var stderrBuf bytes.Buffer
		session.Stdout = &stdoutBuf
		session.Stderr = &stderrBuf

		flags := "-d"

		if config.Core.SpecialDebug {
			flags = ""
		}

		cmd := fmt.Sprintf("/bin/bash %s/.saturn/%s %s", config.Core.RemoteBaseDir, filename, flags)
		if err := session.Run(cmd); err != nil {
			fmt.Println(err.Error())
			fmt.Println(stderrBuf.String())
			session.Close()
			continue
		}
		session.Close()

		if stderrBuf.Len() > 0 {
			log.Println(stderrBuf.String())
		}

		if config.Core.Debug {
			fmt.Println(stdoutBuf.String())
		}

		var response utils.HostResponse
		if err := json.Unmarshal(stdoutBuf.Bytes(), &response); err != nil {
			fmt.Println(err.Error())
			continue
		}

		response.Host = host
		responses = append(responses, &response)
	}
	return responses, nil
}
