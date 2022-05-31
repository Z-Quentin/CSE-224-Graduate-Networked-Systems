package surfstore

import (
	context "context"
	"errors"
	"strings"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

type RPCClient struct {
	MetaStoreAddrs []string
	BaseDir        string
	BlockSize      int
}

func (surfClient *RPCClient) GetBlock(blockHash string, blockStoreAddr string, block *Block) error {
	// connect to the server
	conn, err := grpc.Dial(blockStoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	c := NewBlockStoreClient(conn)

	// perform the call
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, err := c.GetBlock(ctx, &BlockHash{Hash: blockHash})
	if err != nil {
		conn.Close()
		return err
	}
	block.BlockData = b.BlockData
	block.BlockSize = b.BlockSize

	// close the connection
	return conn.Close()
}

func (surfClient *RPCClient) PutBlock(block *Block, blockStoreAddr string, succ *bool) error {
	conn, err := grpc.Dial(blockStoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	c := NewBlockStoreClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := c.PutBlock(ctx, block)
	if err != nil {
		conn.Close()
		return err
	}
	*succ = s.Flag
	return conn.Close()
}

func (surfClient *RPCClient) HasBlocks(blockHashesIn []string, blockStoreAddr string, blockHashesOut *[]string) error {
	conn, err := grpc.Dial(blockStoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	c := NewBlockStoreClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, err := c.HasBlocks(ctx, &BlockHashes{Hashes: blockHashesIn})
	if err != nil {
		conn.Close()
		return err
	}
	*blockHashesOut = b.Hashes

	return conn.Close()
}

func (surfClient *RPCClient) GetFileInfoMap(serverFileInfoMap *map[string]*FileMetaData) error {
	for _, metaStore := range surfClient.MetaStoreAddrs {
		conn, err := grpc.Dial(metaStore, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		c := NewRaftSurfstoreClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		f, err := c.GetFileInfoMap(ctx, &emptypb.Empty{})

		if err != nil {
			if strings.Contains(err.Error(), ERR_SERVER_CRASHED.Error()) {
				continue
			}
			if strings.Contains(err.Error(), ERR_NOT_LEADER.Error()) {
				continue
			}
			conn.Close()
			return err
		}
		*serverFileInfoMap = f.FileInfoMap
		return conn.Close()
	}
	return errors.New("cluster down")
}

func (surfClient *RPCClient) UpdateFile(fileMetaData *FileMetaData, latestVersion *int32) error {
	for _, metaStore := range surfClient.MetaStoreAddrs {
		conn, err := grpc.Dial(metaStore, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		c := NewRaftSurfstoreClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		v, err := c.UpdateFile(ctx, fileMetaData)

		if err != nil {
			if strings.Contains(err.Error(), ERR_SERVER_CRASHED.Error()) {
				continue
			}
			if strings.Contains(err.Error(), ERR_NOT_LEADER.Error()) {
				continue
			}
			conn.Close()
			return err
		}
		*latestVersion = v.Version
		return conn.Close()
	}
	return errors.New("cluster down")
}

func (surfClient *RPCClient) GetBlockStoreAddr(blockStoreAddr *string) error {
	for _, metaStore := range surfClient.MetaStoreAddrs {
		conn, err := grpc.Dial(metaStore, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		c := NewRaftSurfstoreClient(conn)

		// perform the call
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		addr, err := c.GetBlockStoreAddr(ctx, &emptypb.Empty{})
		if err != nil {
			if strings.Contains(err.Error(), ERR_SERVER_CRASHED.Error()) {
				continue
			}
			if !strings.Contains(err.Error(), ERR_NOT_LEADER.Error()) {
				continue
			}
			conn.Close()
			return err
		}
		*blockStoreAddr = addr.Addr
		return conn.Close()
	}
	return errors.New("cluster down")

}

// This line guarantees all method for RPCClient are implemented
var _ ClientInterface = new(RPCClient)

// Create an Surfstore RPC client
func NewSurfstoreRPCClient(addrs []string, baseDir string, blockSize int) RPCClient {
	return RPCClient{
		MetaStoreAddrs: addrs,
		BaseDir:        baseDir,
		BlockSize:      blockSize,
	}
}
