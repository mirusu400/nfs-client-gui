package rpc

import "fmt"

// NFSStatus represents an NFS status code across all versions.
type NFSStatus uint32

const (
	NFSOK          NFSStatus = 0
	NFSERR_PERM    NFSStatus = 1
	NFSERR_NOENT   NFSStatus = 2
	NFSERR_IO      NFSStatus = 5
	NFSERR_NXIO    NFSStatus = 6
	NFSERR_ACCES   NFSStatus = 13
	NFSERR_EXIST   NFSStatus = 17
	NFSERR_XDEV    NFSStatus = 18
	NFSERR_NODEV   NFSStatus = 19
	NFSERR_NOTDIR  NFSStatus = 20
	NFSERR_ISDIR   NFSStatus = 21
	NFSERR_INVAL   NFSStatus = 22
	NFSERR_FBIG    NFSStatus = 27
	NFSERR_NOSPC   NFSStatus = 28
	NFSERR_ROFS    NFSStatus = 30
	NFSERR_NAMETOO NFSStatus = 63
	NFSERR_NOTEMPT NFSStatus = 66
	NFSERR_DQUOT   NFSStatus = 69
	NFSERR_STALE   NFSStatus = 70
	NFSERR_REMOTE  NFSStatus = 71
	// v3 additions
	NFS3ERR_BADHANDLE  NFSStatus = 10001
	NFS3ERR_NOT_SYNC   NFSStatus = 10002
	NFS3ERR_BAD_COOKIE NFSStatus = 10003
	NFS3ERR_NOTSUPP    NFSStatus = 10004
	NFS3ERR_TOOSMALL   NFSStatus = 10005
	NFS3ERR_SERVERFLT  NFSStatus = 10006
	NFS3ERR_BADTYPE    NFSStatus = 10007
	NFS3ERR_JUKEBOX    NFSStatus = 10008
)

var nfsStatusNames = map[NFSStatus]string{
	NFSOK:              "NFS_OK",
	NFSERR_PERM:        "NFSERR_PERM",
	NFSERR_NOENT:       "NFSERR_NOENT",
	NFSERR_IO:          "NFSERR_IO",
	NFSERR_ACCES:       "NFSERR_ACCES",
	NFSERR_EXIST:       "NFSERR_EXIST",
	NFSERR_NOTDIR:      "NFSERR_NOTDIR",
	NFSERR_ISDIR:       "NFSERR_ISDIR",
	NFSERR_INVAL:       "NFSERR_INVAL",
	NFSERR_FBIG:        "NFSERR_FBIG",
	NFSERR_NOSPC:       "NFSERR_NOSPC",
	NFSERR_ROFS:        "NFSERR_ROFS",
	NFSERR_NAMETOO:     "NFSERR_NAMETOOLONG",
	NFSERR_NOTEMPT:     "NFSERR_NOTEMPTY",
	NFSERR_DQUOT:       "NFSERR_DQUOT",
	NFSERR_STALE:       "NFSERR_STALE",
	NFS3ERR_BADHANDLE:  "NFS3ERR_BADHANDLE",
	NFS3ERR_NOT_SYNC:   "NFS3ERR_NOT_SYNC",
	NFS3ERR_BAD_COOKIE: "NFS3ERR_BAD_COOKIE",
	NFS3ERR_NOTSUPP:    "NFS3ERR_NOTSUPP",
	NFS3ERR_TOOSMALL:   "NFS3ERR_TOOSMALL",
	NFS3ERR_SERVERFLT:  "NFS3ERR_SERVERFAULT",
	NFS3ERR_BADTYPE:    "NFS3ERR_BADTYPE",
	NFS3ERR_JUKEBOX:    "NFS3ERR_JUKEBOX",
}

func (s NFSStatus) String() string {
	if name, ok := nfsStatusNames[s]; ok {
		return name
	}
	return fmt.Sprintf("NFS_ERR_%d", uint32(s))
}

// NFSError wraps an NFS status code as a Go error.
type NFSError struct {
	Status NFSStatus
}

func (e *NFSError) Error() string {
	return fmt.Sprintf("nfs: %s", e.Status)
}

// CheckNFSStatus returns an error if status != NFS_OK.
func CheckNFSStatus(status uint32) error {
	if NFSStatus(status) == NFSOK {
		return nil
	}
	return &NFSError{Status: NFSStatus(status)}
}
