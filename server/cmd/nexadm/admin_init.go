package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func init() {
	register("admin", runAdmin)
}

func runAdmin(ctx context.Context, args []string) error {
	reset, err := parseAdminArgs(args)
	if err != nil {
		return err
	}
	username := os.Getenv("NEX_ADMIN_USERNAME")
	password := os.Getenv("NEX_ADMIN_PASSWORD")
	if strings.TrimSpace(username) == "" || password == "" {
		return errors.New("缺少 NEX_ADMIN_USERNAME 或 NEX_ADMIN_PASSWORD")
	}
	if _, err := adminauth.NormalizeUsername(username); err != nil {
		return err
	}
	if err := adminauth.ValidatePassword(password); err != nil {
		return err
	}
	url := strings.TrimSpace(os.Getenv("NEX_DATABASE_URL"))
	if url == "" {
		return errors.New("缺少 NEX_DATABASE_URL")
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer st.Close()
	svc, err := adminauth.NewService(adminauth.NewPGRepo(st), adminauth.Argon2Hasher{}, nil, nil, nil, nil)
	if err != nil {
		return err
	}
	id, err := svc.CreateInitialAdmin(ctx, username, password, reset)
	if err != nil {
		return err
	}
	fmt.Println(id.String())
	return nil
}

func parseAdminArgs(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "init" {
		return false, errors.New("usage: nexadm admin init")
	}
	switch len(args) {
	case 1:
		return false, nil
	case 2:
		if args[1] != "--reset" {
			return false, errors.New("usage: nexadm admin init [--reset]")
		}
		return true, nil
	default:
		return false, errors.New("usage: nexadm admin init [--reset]")
	}
}
