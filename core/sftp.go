package core

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"time"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sftpConnection struct {
	*sftp.Client
	ssh *ssh.Client
}

func (c *sftpConnection) Close() {
	_ = c.Client.Close()
	_ = c.ssh.Close()
}

type remoteFile struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

func (a *Application) sftpClient(r *http.Request) (*sftpConnection, Asset, error) {
	user, _ := requestUser(r)
	cfg, asset, err := a.assetSSHConfig(user, r.PathValue("id"))
	if err != nil {
		return nil, Asset{}, err
	}
	sshClient, err := NewSshClient(cfg)
	if err != nil {
		return nil, Asset{}, err
	}
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, Asset{}, err
	}
	return &sftpConnection{Client: client, ssh: sshClient}, asset, nil
}

func (a *Application) listFiles(w http.ResponseWriter, r *http.Request) {
	client, asset, err := a.sftpClient(r)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer client.Close()
	remotePath := path.Clean(r.URL.Query().Get("path"))
	if remotePath == "." {
		remotePath = "."
	}
	entries, err := client.ReadDir(remotePath)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "读取远程目录失败")
		return
	}
	files := make([]remoteFile, 0, len(entries))
	for _, entry := range entries {
		files = append(files, remoteFile{Name: entry.Name(), Path: path.Join(remotePath, entry.Name()), Size: entry.Size(), Mode: entry.Mode().String(), ModTime: entry.ModTime(), IsDir: entry.IsDir()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	_ = a.store.AppendAudit(a.auditFor(r, "file.list", "asset", asset.ID, true, map[string]any{"path": remotePath}))
	writeJSON(w, http.StatusOK, map[string]any{"path": remotePath, "files": files})
}

func safeRemoteName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || path.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("名称无效")
	}
	return name, nil
}

func (a *Application) createDirectory(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "目录参数无效")
		return
	}
	name, err := safeRemoteName(input.Name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	remotePath := path.Join(path.Clean(input.Path), name)
	client, asset, err := a.sftpClient(r)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer client.Close()
	if err := client.Mkdir(remotePath); err != nil {
		writeAPIError(w, http.StatusConflict, "创建远程目录失败，目录可能已存在")
		return
	}
	_ = a.store.AppendAudit(a.auditFor(r, "file.mkdir", "asset", asset.ID, true, map[string]any{"path": remotePath}))
	writeJSON(w, http.StatusCreated, map[string]string{"path": remotePath})
}

func (a *Application) renameFile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path    string `json:"path"`
		NewName string `json:"new_name"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "重命名参数无效")
		return
	}
	name, err := safeRemoteName(input.NewName)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	source := path.Clean(input.Path)
	if source == "." || source == "/" {
		writeAPIError(w, http.StatusBadRequest, "不能重命名根目录")
		return
	}
	destination := path.Join(path.Dir(source), name)
	client, asset, err := a.sftpClient(r)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer client.Close()
	if _, err := client.Stat(destination); err == nil {
		writeAPIError(w, http.StatusConflict, "目标名称已存在")
		return
	}
	if err := client.Rename(source, destination); err != nil {
		writeAPIError(w, http.StatusBadGateway, "重命名远程文件失败")
		return
	}
	_ = a.store.AppendAudit(a.auditFor(r, "file.rename", "asset", asset.ID, true, map[string]any{"source": source, "destination": destination}))
	writeJSON(w, http.StatusOK, map[string]string{"path": destination})
}

func (a *Application) deleteFile(w http.ResponseWriter, r *http.Request) {
	remotePath := path.Clean(r.URL.Query().Get("path"))
	if remotePath == "." || remotePath == "/" {
		writeAPIError(w, http.StatusBadRequest, "不能删除根目录")
		return
	}
	client, asset, err := a.sftpClient(r)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer client.Close()
	info, err := client.Lstat(remotePath)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "远程文件不存在")
		return
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		err = client.RemoveDirectory(remotePath)
	} else {
		err = client.Remove(remotePath)
	}
	if err != nil {
		writeAPIError(w, http.StatusConflict, "删除失败；目录必须为空且当前账号需要删除权限")
		return
	}
	_ = a.store.AppendAudit(a.auditFor(r, "file.delete", "asset", asset.ID, true, map[string]any{"path": remotePath, "is_dir": info.IsDir()}))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Application) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "必须使用 multipart/form-data 上传")
		return
	}
	part, err := nextFilePart(reader)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer part.Close()
	directory := path.Clean(r.URL.Query().Get("path"))
	filename, nameErr := safeRemoteName(path.Base(strings.ReplaceAll(part.FileName(), `\`, "/")))
	if nameErr != nil {
		writeAPIError(w, http.StatusBadRequest, "文件名无效")
		return
	}
	remotePath := path.Join(directory, filename)
	client, asset, err := a.sftpClient(r)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer client.Close()
	file, err := client.OpenFile(remotePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "远程文件已存在或不可写")
		return
	}
	written, copyErr := io.Copy(file, io.LimitReader(part, a.cfg.MaxUploadBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > a.cfg.MaxUploadBytes {
		_ = client.Remove(remotePath)
		writeAPIError(w, http.StatusBadRequest, "上传失败或文件超过大小限制")
		return
	}
	_ = a.store.AppendAudit(a.auditFor(r, "file.upload", "asset", asset.ID, true, map[string]any{"path": remotePath, "size": written}))
	writeJSON(w, http.StatusCreated, map[string]any{"path": remotePath, "size": written})
}

func nextFilePart(reader *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil, fmt.Errorf("未找到上传文件")
		}
		if part.FormName() == "file" && part.FileName() != "" {
			return part, nil
		}
		part.Close()
	}
}

func (a *Application) downloadFile(w http.ResponseWriter, r *http.Request) {
	remotePath := path.Clean(r.URL.Query().Get("path"))
	if remotePath == "." || remotePath == "/" {
		writeAPIError(w, http.StatusBadRequest, "下载路径无效")
		return
	}
	client, asset, err := a.sftpClient(r)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer client.Close()
	file, err := client.Open(remotePath)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "远程文件不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeAPIError(w, http.StatusBadRequest, "只能下载普通文件")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(path.Base(remotePath))))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, file)
	_ = a.store.AppendAudit(a.auditFor(r, "file.download", "asset", asset.ID, copyErr == nil, map[string]any{"path": remotePath, "size": info.Size()}))
}
