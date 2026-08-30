package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// sessionService implements pb.SessionServiceServer
// (proto/nagipath/api/v1/session.proto): Setup/Login/Logout/ChangePassword.
// GetSession itself stays a plain http.HandlerFunc (session.go) — see
// session.proto's SessionService doc comment for why.
//
// setSessionCookie/clearSessionCookie replace auth.go's old w-based
// setCookie/clearCookie: an RPC method has no http.ResponseWriter, so a
// Set-Cookie response header goes out via grpc.SetHeader instead of
// http.SetCookie — gateway.go's outgoingHeaderMatcher is what turns that
// gRPC metadata key back into a literal, unprefixed Set-Cookie header.
type sessionService struct {
	pb.UnimplementedSessionServiceServer
	s *Server
}

func setSessionCookie(ctx context.Context, secure bool, token string) {
	c := &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secure, MaxAge: int(12 * time.Hour / time.Second),
	}
	_ = grpc.SetHeader(ctx, metadata.Pairs("set-cookie", c.String()))
}

func clearSessionCookie(ctx context.Context, secure bool) {
	c := &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secure, MaxAge: -1,
	}
	_ = grpc.SetHeader(ctx, metadata.Pairs("set-cookie", c.String()))
}

// Setup is reachable only while the database has no users. Once one exists
// it is closed permanently, so it cannot become a way to add an admin later.
func (c *sessionService) Setup(ctx context.Context, req *pb.SetupRequest) (*pb.SessionResponse, error) {
	s := c.s
	if n, _ := s.DB.UserCount(ctx); n > 0 {
		// PermissionDenied (-> 403), not FailedPrecondition (-> 400): the
		// original handler used 403 for "setup is closed", and nothing about
		// this being a precondition failure is worth a different status than
		// what the frontend and its tests already expect.
		return nil, status.Error(codes.PermissionDenied, "This instance is already set up.")
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || len(req.Password) < 12 {
		return nil, status.Error(codes.InvalidArgument, "a username and a password of at least 12 characters are required")
	}
	if req.Password != req.Confirm {
		return nil, status.Error(codes.InvalidArgument, "the two passwords do not match")
	}
	id, err := s.DB.CreateUser(ctx, username, req.Password, "admin", username, false)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	s.DB.Audit(ctx, &id, "user.create", "user", &id, username)
	token, err := s.DB.NewSession(ctx, id, userAgentOf(ctx), remoteAddrOf(ctx))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	setSessionCookie(ctx, s.Secure, token)
	u := store.User{ID: id, Username: username, Role: "admin"}
	return s.sessionResponse(ctx, u, token), nil
}

func (c *sessionService) Login(ctx context.Context, req *pb.LoginRequest) (*pb.SessionResponse, error) {
	s := c.s
	username := strings.TrimSpace(req.Username)
	addr := remoteAddrOf(ctx)
	// Checked before Authenticate does any argon2id work: the point of a rate
	// limit is refusing a guess before paying for it, not after — the same
	// ordering probe's rate limit uses before a Probe's request goes out.
	if limited, err := s.DB.LoginRateLimited(ctx, username); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else if limited {
		s.DB.AuditDetail(ctx, nil, "auth.login", "user", nil, username, nil, "denied", addr)
		return nil, status.Error(codes.ResourceExhausted, "too many failed attempts for this account; try again later")
	}
	u, err := s.DB.Authenticate(ctx, username, req.Password)
	if err != nil {
		// The message is deliberately identical for a bad password and a
		// missing user: which one it was is not the caller's business to learn.
		s.DB.AuditDetail(ctx, nil, "auth.login", "user", nil, username, nil, "failure", addr)
		return nil, status.Error(codes.Unauthenticated, "invalid username or password")
	}
	token, err := s.DB.NewSession(ctx, u.ID, userAgentOf(ctx), addr)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.DB.AuditDetail(ctx, &u.ID, "auth.login", "user", &u.ID, u.Username, nil, "success", addr)
	setSessionCookie(ctx, s.Secure, token)
	return s.sessionResponse(ctx, u, token), nil
}

func (c *sessionService) Logout(ctx context.Context, _ *pb.Empty) (*pb.Ok, error) {
	s := c.s
	if tok := sessionTokenOf(ctx); tok != "" {
		s.DB.EndSession(ctx, tok)
	}
	clearSessionCookie(ctx, s.Secure)
	return &pb.Ok{Ok: true}, nil
}

func (c *sessionService) ChangePassword(ctx context.Context, req *pb.ChangePasswordRequest) (*pb.SessionResponse, error) {
	s := c.s
	u := userOf(ctx)
	if len(req.New) < 12 {
		return nil, status.Error(codes.InvalidArgument, "the new password must be at least 12 characters")
	}
	if req.New != req.Confirm {
		return nil, status.Error(codes.InvalidArgument, "the two passwords do not match")
	}
	// Requiring the current password stops a borrowed logged-in browser from
	// locking the real owner out. Skipped for a forced first-login change,
	// since the temp password was handed out by an admin, not chosen by the
	// person changing it.
	if !u.MustChangePassword {
		if _, err := s.DB.Authenticate(ctx, u.Username, req.Current); err != nil {
			return nil, status.Error(codes.InvalidArgument, "the current password is not correct")
		}
	}
	if err := s.DB.SetPassword(ctx, u.ID, req.New); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.DB.Audit(ctx, &u.ID, "user.password_change", "user", &u.ID, u.Username)
	if err := s.DB.EndAllSessions(ctx, u.ID); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	token, err := s.DB.NewSession(ctx, u.ID, userAgentOf(ctx), remoteAddrOf(ctx))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	setSessionCookie(ctx, s.Secure, token)
	u.MustChangePassword = false
	return s.sessionResponse(ctx, u, token), nil
}
