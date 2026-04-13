// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"encoding/hex"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/node/utils/password"
)

// strongPassword defines a password used for the following tests that
// scores high enough to pass the password strength scoring system
var strongPassword = "N_+=_jJ;^(<;{4,:*m6CET}'&N;83FYK.wtNpwp-Jt" // #nosec G101

func TestServiceListNoUsers(t *testing.T) {
	require := require.New(t)

	ks := New(log.NewNoOpLogger(), memdb.New())
	s := service{ks: ks.(*keystore)}

	reply := ListUsersReply{}
	require.NoError(s.ListUsers(nil, nil, &reply))
	require.Empty(reply.Users)
}

func TestServiceCreateUser(t *testing.T) {
	require := require.New(t)

	ks := New(log.NewNoOpLogger(), memdb.New())
	s := service{ks: ks.(*keystore)}

	{
		require.NoError(s.CreateUser(nil, &apitypes.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &apitypes.EmptyReply{}))
	}

	{
		reply := ListUsersReply{}
		require.NoError(s.ListUsers(nil, nil, &reply))
		require.Len(reply.Users, 1)
		require.Equal("bob", reply.Users[0])
	}
}

// genStr returns a string of given length
func genStr(n int) string {
	b := make([]byte, n)
	rand.Read(b) // #nosec G404
	return hex.EncodeToString(b)[:n]
}

// TestServiceCreateUserArgsCheck generates excessively long usernames or
// passwords to assure the sanity checks on string length are not exceeded
func TestServiceCreateUserArgsCheck(t *testing.T) {
	require := require.New(t)

	ks := New(log.NewNoOpLogger(), memdb.New())
	s := service{ks: ks.(*keystore)}

	{
		reply := apitypes.EmptyReply{}
		err := s.CreateUser(nil, &apitypes.UserPass{
			Username: genStr(maxUserLen + 1),
			Password: strongPassword,
		}, &reply)
		require.ErrorIs(err, errUserMaxLength)
	}

	{
		reply := apitypes.EmptyReply{}
		err := s.CreateUser(nil, &apitypes.UserPass{
			Username: "shortuser",
			Password: genStr(maxUserLen + 1),
		}, &reply)
		require.ErrorIs(err, password.ErrPassMaxLength)
	}

	{
		reply := ListUsersReply{}
		require.NoError(s.ListUsers(nil, nil, &reply))
		require.Empty(reply.Users)
	}
}

// TestServiceCreateUserWeakPassword tests creating a new user with a weak
// password to ensure the password strength check is working
func TestServiceCreateUserWeakPassword(t *testing.T) {
	require := require.New(t)

	ks := New(log.NewNoOpLogger(), memdb.New())
	s := service{ks: ks.(*keystore)}

	{
		reply := apitypes.EmptyReply{}
		err := s.CreateUser(nil, &apitypes.UserPass{
			Username: "bob",
			Password: "weak",
		}, &reply)
		require.ErrorIs(err, password.ErrWeakPassword)
	}
}

func TestServiceCreateDuplicate(t *testing.T) {
	require := require.New(t)

	ks := New(log.NewNoOpLogger(), memdb.New())
	s := service{ks: ks.(*keystore)}

	{
		require.NoError(s.CreateUser(nil, &apitypes.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &apitypes.EmptyReply{}))
	}

	{
		err := s.CreateUser(nil, &apitypes.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &apitypes.EmptyReply{})
		require.ErrorIs(err, errUserAlreadyExists)
	}
}

func TestServiceCreateUserNoName(t *testing.T) {
	require := require.New(t)

	ks := New(log.NewNoOpLogger(), memdb.New())
	s := service{ks: ks.(*keystore)}

	reply := apitypes.EmptyReply{}
	err := s.CreateUser(nil, &apitypes.UserPass{
		Password: strongPassword,
	}, &reply)
	require.ErrorIs(err, errEmptyUsername)
}

func TestServiceUseBlockchainDB(t *testing.T) {
	require := require.New(t)

	ks := New(log.NewNoOpLogger(), memdb.New())
	s := service{ks: ks.(*keystore)}

	{
		require.NoError(s.CreateUser(nil, &apitypes.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &apitypes.EmptyReply{}))
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
		require.NoError(err)
		require.NoError(db.Put([]byte("hello"), []byte("world")))
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
		require.NoError(err)
		val, err := db.Get([]byte("hello"))
		require.NoError(err)
		require.Equal([]byte("world"), val)
	}
}

func TestServiceExportImport(t *testing.T) {
	require := require.New(t)

	encodings := []formatting.Encoding{formatting.Hex}
	for _, encoding := range encodings {
		ks := New(log.NewNoOpLogger(), memdb.New())
		s := service{ks: ks.(*keystore)}

		{
			require.NoError(s.CreateUser(nil, &apitypes.UserPass{
				Username: "bob",
				Password: strongPassword,
			}, &apitypes.EmptyReply{}))
		}

		{
			db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
			require.NoError(err)
			require.NoError(db.Put([]byte("hello"), []byte("world")))
		}

		exportArgs := ExportUserArgs{
			UserPass: apitypes.UserPass{
				Username: "bob",
				Password: strongPassword,
			},
			Encoding: encoding,
		}
		exportReply := ExportUserReply{}
		require.NoError(s.ExportUser(nil, &exportArgs, &exportReply))

		newKS := New(log.NewNoOpLogger(), memdb.New())
		newS := service{ks: newKS.(*keystore)}

		{
			err := newS.ImportUser(nil, &ImportUserArgs{
				UserPass: apitypes.UserPass{
					Username: "bob",
					Password: "",
				},
				User: exportReply.User,
			}, &apitypes.EmptyReply{})
			require.ErrorIs(err, errIncorrectPassword)
		}

		{
			err := newS.ImportUser(nil, &ImportUserArgs{
				UserPass: apitypes.UserPass{
					Username: "",
					Password: "strongPassword",
				},
				User: exportReply.User,
			}, &apitypes.EmptyReply{})
			require.ErrorIs(err, errEmptyUsername)
		}

		{
			require.NoError(newS.ImportUser(nil, &ImportUserArgs{
				UserPass: apitypes.UserPass{
					Username: "bob",
					Password: strongPassword,
				},
				User:     exportReply.User,
				Encoding: encoding,
			}, &apitypes.EmptyReply{}))
		}

		{
			db, err := newKS.GetDatabase(ids.Empty, "bob", strongPassword)
			require.NoError(err)
			val, err := db.Get([]byte("hello"))
			require.NoError(err)
			require.Equal([]byte("world"), val)
		}
	}
}

func TestServiceDeleteUser(t *testing.T) {
	testUser := "testUser"
	password := "passwTest@fake01ordX" // 20+ chars for Fair strength
	tests := []struct {
		desc        string
		setup       func(ks *keystore) error
		request     *apitypes.UserPass
		want        *apitypes.EmptyReply
		expectedErr error
	}{
		{
			desc:        "empty user name case",
			request:     &apitypes.UserPass{},
			expectedErr: errEmptyUsername,
		},
		{
			desc:        "user not exists case",
			request:     &apitypes.UserPass{Username: "dummy"},
			expectedErr: errNonexistentUser,
		},
		{
			desc: "user exists and invalid password case",
			setup: func(ks *keystore) error {
				s := service{ks: ks}
				return s.CreateUser(nil, &apitypes.UserPass{Username: testUser, Password: password}, &apitypes.EmptyReply{})
			},
			request:     &apitypes.UserPass{Username: testUser, Password: "password"},
			expectedErr: errIncorrectPassword,
		},
		{
			desc: "user exists and valid password case",
			setup: func(ks *keystore) error {
				s := service{ks: ks}
				return s.CreateUser(nil, &apitypes.UserPass{Username: testUser, Password: password}, &apitypes.EmptyReply{})
			},
			request: &apitypes.UserPass{Username: testUser, Password: password},
			want:    &apitypes.EmptyReply{},
		},
		{
			desc: "delete a user, imported from import api case",
			setup: func(ks *keystore) error {
				s := service{ks: ks}

				reply := apitypes.EmptyReply{}
				if err := s.CreateUser(nil, &apitypes.UserPass{Username: testUser, Password: password}, &reply); err != nil {
					return err
				}

				// created data in bob db
				db, err := ks.GetDatabase(ids.Empty, testUser, password)
				if err != nil {
					return err
				}

				return db.Put([]byte("hello"), []byte("world"))
			},
			request: &apitypes.UserPass{Username: testUser, Password: password},
			want:    &apitypes.EmptyReply{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			require := require.New(t)

			ksIntf := New(log.NewNoOpLogger(), memdb.New())
			ks := ksIntf.(*keystore)
			s := service{ks: ks}

			if tt.setup != nil {
				require.NoError(tt.setup(ks))
			}
			got := &apitypes.EmptyReply{}
			err := s.DeleteUser(nil, tt.request, got)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.want, got)
			require.NotContains(ks.usernameToPassword, testUser) // delete is successful
		})
	}
}
