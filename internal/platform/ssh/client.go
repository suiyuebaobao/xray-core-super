// Package ssh 提供远程服务器连接和执行命令的能力。
package ssh

import (
	"bytes"
	"fmt"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// Client 远程 SSH 客户端。
type Client struct {
	conn   *ssh.Client
	host   string
	port   int
	user   string
	passwd string
}

// Config SSH 连接配置。
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

// New 创建 SSH 客户端。
func New(cfg Config) *Client {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	return &Client{
		host:   cfg.Host,
		port:   cfg.Port,
		user:   cfg.User,
		passwd: cfg.Password,
	}
}

// Connect 连接到远程服务器。
func (c *Client) Connect() error {
	config := &ssh.ClientConfig{
		User: c.user,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.passwd),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	c.conn = conn
	return nil
}

// Close 关闭 SSH 连接。
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// Exec 在远程服务器执行命令，返回 stdout。
func (c *Client) Exec(cmd string) (string, error) {
	if c.conn == nil {
		return "", fmt.Errorf("not connected")
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(cmd); err != nil {
		return stdout.String(), fmt.Errorf("run %q: %w, stderr: %s", cmd, err, stderr.String())
	}

	return stdout.String(), nil
}

// Upload 上传文件到远程服务器。
func (c *Client) Upload(remotePath string, data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	// 使用 scp 协议上传文件
	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		// scp 格式: mode size filename
		fmt.Fprintf(w, "C0644 %d %s\n", len(data), filepath.Base(remotePath))
		w.Write(data)
		fmt.Fprint(w, "\x00") // 结束标记
	}()

	if err := session.Run("scp -t " + remotePath); err != nil {
		return fmt.Errorf("scp upload: %w", err)
	}

	return nil
}
