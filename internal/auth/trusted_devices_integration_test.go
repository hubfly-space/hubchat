//go:build integration

package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestTrustedDeviceSkipsOnlyTOTPAndIsRevocable(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "trusted-device@example.com")

	secret, _, err := svc.BeginTOTPEnrolment(ctx, user.ID, "Hubchat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteTOTPEnrolment(ctx, user.ID, secret, currentCode(t, secret)); err != nil {
		t.Fatal(err)
	}

	challenge, err := svc.SignIn(ctx, user.Email, password, "trusted-browser", "")
	if err != nil || challenge.Challenge == nil {
		t.Fatalf("sign-in did not require TOTP: result=%+v err=%v", challenge, err)
	}
	verified, err := svc.VerifyTOTPChallengeWithTrust(ctx, challenge.Challenge.Token, currentCode(t, secret), "trusted-browser", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if verified.TrustedDevice == nil || verified.Session == nil {
		t.Fatalf("trusted verification did not issue both credentials: %+v", verified)
	}

	trusted, err := svc.SignInWithTrustedDevice(ctx, user.Email, password, "trusted-browser", "", verified.TrustedDevice.Token)
	if err != nil || trusted.Challenge != nil || trusted.Session == nil {
		t.Fatalf("trusted device did not skip TOTP: result=%+v err=%v", trusted, err)
	}
	secondChallenge, err := svc.SignIn(ctx, user.Email, password, "second-browser", "")
	if err != nil || secondChallenge.Challenge == nil {
		t.Fatalf("second sign-in did not require TOTP: result=%+v err=%v", secondChallenge, err)
	}
	second, err := svc.VerifyTOTPChallengeWithTrust(ctx, secondChallenge.Challenge.Token, currentCode(t, secret), "second-browser", "", true)
	if err != nil || second.TrustedDevice == nil {
		t.Fatalf("second trusted device was not issued: result=%+v err=%v", second, err)
	}

	devices, err := svc.ListTrustedDevices(ctx, user.ID, verified.TrustedDevice.Token)
	currentCount := 0
	for _, device := range devices {
		if device.Current {
			currentCount++
		}
	}
	if err != nil || len(devices) != 2 || currentCount != 1 {
		t.Fatalf("trusted device list = %+v err=%v", devices, err)
	}
	firstPage, err := svc.ListTrustedDevicesPage(ctx, user.ID, verified.TrustedDevice.Token, time.Time{}, "", 1)
	if err != nil || len(firstPage) != 1 {
		t.Fatalf("trusted device first page = %+v err=%v", firstPage, err)
	}
	secondPage, err := svc.ListTrustedDevicesPage(ctx, user.ID, verified.TrustedDevice.Token, firstPage[0].CreatedAt, firstPage[0].ID, 1)
	if err != nil || len(secondPage) != 1 || secondPage[0].ID == firstPage[0].ID {
		t.Fatalf("trusted device second page = %+v err=%v", secondPage, err)
	}
	var verifiedDeviceID string
	for _, device := range devices {
		if device.Current {
			verifiedDeviceID = device.ID
			break
		}
	}
	if verifiedDeviceID == "" {
		t.Fatal("could not identify the trusted device for the original credential")
	}
	attacker := seedUser(t, ctx, svc, "trusted-attacker@example.com")
	if err := svc.RevokeTrustedDevice(ctx, attacker.ID, verifiedDeviceID); !errors.Is(err, auth.ErrTrustedDeviceNotFound) {
		t.Fatalf("cross-user revoke error = %v, want ErrTrustedDeviceNotFound", err)
	}
	if err := svc.RevokeTrustedDevice(ctx, user.ID, verifiedDeviceID); err != nil {
		t.Fatal(err)
	}
	afterRevoke, err := svc.SignInWithTrustedDevice(ctx, user.Email, password, "trusted-browser", "", verified.TrustedDevice.Token)
	if err != nil || afterRevoke.Challenge == nil {
		t.Fatalf("revoked device still bypassed TOTP: result=%+v err=%v", afterRevoke, err)
	}
}
