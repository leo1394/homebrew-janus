// Package janus defines the shared device/runtime boundary for optional extensions.
// Extensions must use these services rather than creating a second SSH master pool.
package janus

import (
    "context"
    "regexp"
)

type Device struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Platform string `json:"platform"`
    Type string `json:"type"`
    Connected *bool `json:"connected"`
    Ready *bool `json:"ready"`
}

type Profile struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Host string `json:"host"`
    User string `json:"user"`
    Port int `json:"port"`
    PersistSeconds int `json:"persistSeconds"`
    CredentialSlot string `json:"credentialSlot,omitempty"`
    AccountID string `json:"accountId,omitempty"`
    Database string `json:"database,omitempty"`
    DatabaseTLS string `json:"databaseTLS,omitempty"`
    DatabaseCA string `json:"databaseCA,omitempty"`
    Platform string `json:"platform,omitempty"`
    Type string `json:"type,omitempty"`
    AgentTransport string `json:"agentTransport,omitempty"`
    InstanceName string `json:"instanceName,omitempty"`
    InstanceID string `json:"instanceId,omitempty"`
    LastSession string `json:"lastSession,omitempty"`
}

// Runtime never grants Agent permission. A Confidential adapter must validate its
// own lease before invoking runtime operations; device readiness is not a lease.
type Runtime interface {
    Ensure(context.Context, Profile) error
    Disconnect(context.Context, Profile) error
    Connected(context.Context, Profile) (bool, error)
}

type DeviceProvider interface {
    Devices(context.Context) ([]Device, error)
}

// SSHRuntime is supplied by the single connection owner, including session I/O.
type SSHRuntime interface {
    Runtime
    Attach(context.Context, Profile) error
    DeleteCredential(context.Context, Profile) error
}

var agentTypePattern=regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
func ValidAgentType(kind string)bool{return agentTypePattern.MatchString(kind)}
func (p Profile) IsAgent() bool { return (p.Platform=="ai" && p.Type=="agent") || ((p.Platform=="Agenux" || p.Platform=="a2x") && ValidAgentType(p.Type)) }
func (p Profile) DevicePlatform() string { if p.IsAgent(){return "a2x"};if p.Platform=="" {return "terminal"};return p.Platform }
func (p Profile) DeviceType() string { if p.Platform=="ai"&&p.Type=="agent"{return "pi"};if p.Type=="" {return "ssh"};return p.Type }

// AgentMFATarget is display context only; remote identity is verified after MFA.
type AgentMFATarget struct { Target string; Type string; InstanceName string }

func (p Profile) IsMySQL() bool { return p.Platform=="database" && p.Type=="mysql" }

// PlatformOrder is shared by device lists, selectors and completion candidates.
func PlatformOrder(platform string)int{switch platform{case "terminal":return 0;case "a2x":return 1;case "database":return 2;default:return 3}}

// AgentInstance is an SSH-authenticated discovery snapshot, not MFA authorization.
type AgentInstance struct {
    Name string `json:"name"`
    Type string `json:"type"`
    Status string `json:"status"`
    InstanceID string `json:"instanceId,omitempty"`
}
