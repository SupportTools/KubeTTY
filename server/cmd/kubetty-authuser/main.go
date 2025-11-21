package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	log.SetFlags(0)
	if os.Getenv("SESSION_ID") == "" {
		_ = os.Setenv("SESSION_ID", "kubetty-authuser")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	poolConfig, err := cfg.ConnConfig()
	if err != nil {
		log.Fatalf("build pool config: %v", err)
	}

	db, err := auth.NewStore(context.Background(), poolConfig)
	if err != nil {
		log.Fatalf("connect auth store: %v", err)
	}
	defer db.Close()

	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "create":
		handleCreate(db, os.Args[2:])
	case "update-password":
		handleUpdatePassword(db, os.Args[2:])
	case "list":
		handleList(db)
	case "set-active":
		handleSetActive(db, os.Args[2:])
	default:
		usage()
	}
}

func handleCreate(store auth.Store, args []string) {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	username := fs.String("username", "", "username to create")
	password := fs.String("password", "", "password to hash")
	active := fs.Bool("active", true, "mark user as active")
	fs.Parse(args)
	if strings.TrimSpace(*username) == "" || *password == "" {
		log.Fatalf("username and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	user := auth.User{
		Username:     strings.TrimSpace(*username),
		PasswordHash: hash,
		IsActive:     *active,
	}
	if err := store.CreateUser(context.Background(), user); err != nil {
		log.Fatalf("create user: %v", err)
	}
	fmt.Printf("created user %s\n", user.Username)
}

func handleUpdatePassword(store auth.Store, args []string) {
	fs := flag.NewFlagSet("update-password", flag.ExitOnError)
	username := fs.String("username", "", "username to update")
	password := fs.String("password", "", "new password")
	fs.Parse(args)
	if strings.TrimSpace(*username) == "" || *password == "" {
		log.Fatalf("username and password are required")
	}
	user, err := store.GetUserByUsername(context.Background(), strings.TrimSpace(*username))
	if err != nil {
		log.Fatalf("lookup user: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	if err := store.UpdateUserPassword(context.Background(), user.ID, hash); err != nil {
		log.Fatalf("update password: %v", err)
	}
	fmt.Printf("updated password for %s\n", user.Username)
}

func handleSetActive(store auth.Store, args []string) {
	fs := flag.NewFlagSet("set-active", flag.ExitOnError)
	username := fs.String("username", "", "username to update")
	active := fs.Bool("active", true, "whether the account is active")
	fs.Parse(args)
	if strings.TrimSpace(*username) == "" {
		log.Fatalf("username is required")
	}
	user, err := store.GetUserByUsername(context.Background(), strings.TrimSpace(*username))
	if err != nil {
		log.Fatalf("lookup user: %v", err)
	}
	if err := store.SetUserActive(context.Background(), user.ID, *active); err != nil {
		log.Fatalf("set active: %v", err)
	}
	state := "disabled"
	if *active {
		state = "enabled"
	}
	fmt.Printf("user %s is now %s\n", user.Username, state)
}

func handleList(store auth.Store) {
	users, err := store.ListUsers(context.Background())
	if err != nil {
		log.Fatalf("list users: %v", err)
	}
	if len(users) == 0 {
		fmt.Println("no users")
		return
	}
	for _, u := range users {
		active := "inactive"
		if u.IsActive {
			active = "active"
		}
		fmt.Printf("%s\t%s\t%s\n", u.ID, u.Username, active)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  kubetty-authuser <command> [flags]

Commands:
  create          --username <name> --password <secret> [--active]
  update-password --username <name> --password <secret>
  set-active      --username <name> [--active=true|false]
  list

Environment:
  SESSION_ID must be set (the helper defaults to kubetty-authuser if absent).
  CNPG_* variables must point to the database used by the server.
`)
	os.Exit(1)
}
