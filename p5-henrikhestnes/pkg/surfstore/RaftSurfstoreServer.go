package surfstore

import (
	context "context"
	//"log"
	"math"
	"sync"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	"google.golang.org/grpc"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

type RaftSurfstore struct {
	// TODO add any fields you need
	isLeader bool
	term     int64
	log      []*UpdateOperation

	metaStore *MetaStore

	commitIndex    int64
	pendingCommits []chan bool

	lastApplied int64

	// Server Info
	ip       string
	ipList   []string
	serverId int64

	// Leader protection
	isLeaderMutex sync.RWMutex
	// isLeaderCond  *sync.Cond

	rpcClients []RaftSurfstoreClient

	/*--------------- Chaos Monkey --------------*/
	isCrashed      bool
	isCrashedMutex *sync.RWMutex
	notCrashedCond *sync.Cond

	UnimplementedRaftSurfstoreServer
}

func (s *RaftSurfstore) GetFileInfoMap(ctx context.Context, empty *emptypb.Empty) (*FileInfoMap, error) {
	s.isCrashedMutex.RLock()
	isCrashed := s.isCrashed
	s.isCrashedMutex.RUnlock()
	if isCrashed {
		return nil, ERR_SERVER_CRASHED
	}

	s.isLeaderMutex.RLock()
	isLeader := s.isLeader
	s.isLeaderMutex.RUnlock()
	if !isLeader {
		return nil, ERR_NOT_LEADER
	}

	for {
		majorityAlive, _ := s.SendHeartbeat(ctx, empty)
		if majorityAlive.Flag {
			break
		}
	}

	//if a majority of the nodes are working, should return the correct answer;
	//if a majority of the nodes are crashed, should block until a majority recover.

	return s.metaStore.GetFileInfoMap(ctx, empty)
}

func (s *RaftSurfstore) GetBlockStoreAddr(ctx context.Context, empty *emptypb.Empty) (*BlockStoreAddr, error) {
	s.isCrashedMutex.RLock()
	isCrashed := s.isCrashed
	s.isCrashedMutex.RUnlock()
	if isCrashed {
		return nil, ERR_SERVER_CRASHED
	}

	s.isLeaderMutex.RLock()
	isLeader := s.isLeader
	s.isLeaderMutex.RUnlock()
	if !isLeader {
		return nil, ERR_NOT_LEADER
	}

	for {
		majorityAlive, _ := s.SendHeartbeat(ctx, empty)
		if majorityAlive.Flag {
			break
		}
	}

	//if a majority of the nodes are working, should return the correct answer;
	//if a majority of the nodes are crashed, should block until a majority recover.

	return s.metaStore.GetBlockStoreAddr(ctx, empty)
}

func (s *RaftSurfstore) UpdateFile(ctx context.Context, filemeta *FileMetaData) (*Version, error) {
	op := UpdateOperation{
		Term:         s.term,
		FileMetaData: filemeta,
	}

	s.isCrashedMutex.RLock()
	isCrashed := s.isCrashed
	s.isCrashedMutex.RUnlock()
	if isCrashed {
		return nil, ERR_SERVER_CRASHED
	}

	s.isLeaderMutex.RLock()
	isLeader := s.isLeader
	s.isLeaderMutex.RUnlock()
	if !isLeader {
		return nil, ERR_NOT_LEADER
	}

	s.log = append(s.log, &op)
	committed := make(chan bool)
	s.pendingCommits = append(s.pendingCommits, committed)

	go s.attemptCommit()

	//if a majority of the nodes are working, should return the correct answer;
	//if a majority of the nodes are crashed, should block until a majority recover.

	success := <-committed
	if success {
		s.lastApplied = s.commitIndex
		return s.metaStore.UpdateFile(ctx, filemeta)
	}

	return nil, ERR_SERVER_CRASHED
}

func (s *RaftSurfstore) attemptCommit() {
	targetIdx := s.commitIndex + 1
	pendingIdx := int64(len(s.pendingCommits) - 1)
	//targetIdx := int64(len(s.log) - 1)
	commitChan := make(chan *AppendEntryOutput, len(s.ipList))
	for idx, _ := range s.ipList {
		if int64(idx) == s.serverId {
			continue
		}
		go s.commitEntry(int64(idx), targetIdx, commitChan)
	}

	commitCount := 1
	for {
		// TODO handle crashed nodes- done
		commit := <-commitChan
		s.isCrashedMutex.RLock()
		isCrashed := s.isCrashed
		s.isCrashedMutex.RUnlock()
		if isCrashed {
			s.pendingCommits[pendingIdx] <- false
			break
		}

		if commit != nil && commit.Success {
			commitCount++
		}
		if commitCount > len(s.ipList)/2 {
			s.commitIndex = targetIdx
			s.pendingCommits[pendingIdx] <- true
			break
		}
	}
}

func (s *RaftSurfstore) commitEntry(serverIdx, entryIdx int64, commitChan chan *AppendEntryOutput) {
	for {
		s.isCrashedMutex.RLock()
		isCrashed := s.isCrashed
		s.isCrashedMutex.RUnlock()
		if isCrashed {
			commitChan <- &AppendEntryOutput{Success: false}
		}

		addr := s.ipList[serverIdx]
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return
		}
		client := NewRaftSurfstoreClient(conn)

		// TODO create correct AppendEntryInput from s.nextIndex, etc
		var prevLogTerm int64
		if entryIdx == 0 {
			prevLogTerm = 0
		} else {
			prevLogTerm = s.log[entryIdx-1].Term
		}
		input := &AppendEntryInput{
			Term:         s.term,
			PrevLogIndex: entryIdx - 1,
			PrevLogTerm:  prevLogTerm,
			Entries:      s.log[:entryIdx+1],
			LeaderCommit: s.commitIndex,
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		output, _ := client.AppendEntries(ctx, input)

		if output != nil {
			if output.Success {
				commitChan <- output
				return
			}
		}
		// TODO update state. s.nextIndex, etc
		//return
		// TODO handle crashed/ non success cases
	}
}

//1. Reply false if term < currentTerm (§5.1)
//2. Reply false if log doesn’t contain an entry at prevLogIndex whose term
//matches prevLogTerm (§5.3)
//3. If an existing entry conflicts with a new one (same index but different
//terms), delete the existing entry and all that follow it (§5.3)
//4. Append any new entries not already in the log
//5. If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index
//of last new entry)
func (s *RaftSurfstore) AppendEntries(ctx context.Context, input *AppendEntryInput) (*AppendEntryOutput, error) {
	output := &AppendEntryOutput{
		Success:      false,
		MatchedIndex: -1,
	}

	s.isCrashedMutex.RLock()
	isCrashed := s.isCrashed
	s.isCrashedMutex.RUnlock()
	if isCrashed {
		return output, ERR_SERVER_CRASHED
	}

	if input.Term > s.term {
		s.term = input.Term
		s.isLeaderMutex.Lock()
		s.isLeader = false
		s.isLeaderMutex.Unlock()
	}

	//1. Reply false if term < currentTerm (§5.1)
	if input.Term < s.term { //TODO wrong??
		return output, nil
	}
	//2. Reply false if log doesn’t contain an entry at prevLogIndex whose term
	//matches prevLogTerm (§5.3)
	// if len(s.log) < int(input.PrevLogIndex) {
	// 	return output, nil
	// }
	// if len(s.log) >= 1 {
	// 	if s.log[input.PrevLogIndex].Term != input.PrevLogTerm {
	// 		return output, nil
	// 	}
	// }
	// if len(input.Entries) != 0 && len(s.log) != 0 && len(s.log) > int(input.PrevLogIndex) {
	// 	if s.log[input.PrevLogIndex].Term != input.PrevLogTerm {
	// 		return output, nil
	// 	}
	// }

	//3. If an existing entry conflicts with a new one (same index but different
	//terms), delete the existing entry and all that follow it (§5.3)
	for index, logEntry := range s.log {
		s.lastApplied = int64(index - 1)
		if len(input.Entries) < index+1 {
			s.log = s.log[:index]
			input.Entries = make([]*UpdateOperation, 0)
			break
		}
		if logEntry != input.Entries[index] {
			s.log = s.log[:index]
			input.Entries = input.Entries[index:]
			break
		}
		if len(s.log) == index+1 { //last iteration, all match
			input.Entries = input.Entries[index+1:]
		}
	}
	//4. Append any new entries not already in the log
	s.log = append(s.log, input.Entries...)

	//5. If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index
	//of last new entry)
	// TODO only do this if leaderCommit > commitIndex
	if input.LeaderCommit > s.commitIndex {
		s.commitIndex = int64(math.Min(float64(input.LeaderCommit), float64(len(s.log)-1)))

		for s.lastApplied < s.commitIndex {
			s.lastApplied++
			entry := s.log[s.lastApplied]
			s.metaStore.UpdateFile(ctx, entry.FileMetaData)
		}
	}
	output.Success = true
	return output, nil
}

// This should set the leader status and any related variables as if the node has just won an election
func (s *RaftSurfstore) SetLeader(ctx context.Context, _ *emptypb.Empty) (*Success, error) {
	s.isCrashedMutex.RLock()
	isCrashed := s.isCrashed
	s.isCrashedMutex.RUnlock()
	if isCrashed {
		return &Success{Flag: false}, ERR_SERVER_CRASHED
	}

	s.term++
	s.isLeaderMutex.Lock()
	s.isLeader = true
	s.isLeaderMutex.Unlock()
	return &Success{Flag: true}, nil
}

// Send a 'Heartbeat" (AppendEntries with no log entries) to the other servers
// Only leaders send heartbeats, if the node is not the leader you can return Success = false
func (s *RaftSurfstore) SendHeartbeat(ctx context.Context, _ *emptypb.Empty) (*Success, error) {
	s.isCrashedMutex.RLock()
	isCrashed := s.isCrashed
	s.isCrashedMutex.RUnlock()
	if isCrashed {
		return &Success{Flag: false}, ERR_SERVER_CRASHED
	}

	s.isLeaderMutex.RLock()
	isLeader := s.isLeader
	s.isLeaderMutex.RUnlock()
	if !isLeader {
		return &Success{Flag: false}, ERR_NOT_LEADER
	}

	majorityAlive := false
	aliveCount := 1
	for idx, addr := range s.ipList {
		if int64(idx) == s.serverId {
			continue
		}

		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return &Success{Flag: false}, nil
		}
		client := NewRaftSurfstoreClient(conn)

		// TODO create correct AppendEntryInput from s.nextIndex, etc
		var prevLogTerm int64
		if s.commitIndex == -1 {
			prevLogTerm = 0
		} else {
			prevLogTerm = s.log[s.commitIndex].Term
		}
		input := &AppendEntryInput{
			Term:         s.term,
			PrevLogTerm:  prevLogTerm,
			PrevLogIndex: s.commitIndex,
			// TODO figure out which entries to send
			Entries:      s.log,
			LeaderCommit: s.commitIndex,
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		output, _ := client.AppendEntries(ctx, input)
		if output != nil {
			aliveCount++
			if aliveCount > len(s.ipList)/2 {
				majorityAlive = true
			}
		}
	}
	return &Success{Flag: majorityAlive}, nil
}

func (s *RaftSurfstore) Crash(ctx context.Context, _ *emptypb.Empty) (*Success, error) {
	s.isCrashedMutex.Lock()
	s.isCrashed = true
	s.isCrashedMutex.Unlock()

	return &Success{Flag: true}, nil
}

func (s *RaftSurfstore) Restore(ctx context.Context, _ *emptypb.Empty) (*Success, error) {
	s.isCrashedMutex.Lock()
	s.isCrashed = false
	s.notCrashedCond.Broadcast()
	s.isCrashedMutex.Unlock()

	return &Success{Flag: true}, nil
}

func (s *RaftSurfstore) IsCrashed(ctx context.Context, _ *emptypb.Empty) (*CrashedState, error) {
	return &CrashedState{IsCrashed: s.isCrashed}, nil
}

func (s *RaftSurfstore) GetInternalState(ctx context.Context, empty *emptypb.Empty) (*RaftInternalState, error) {
	fileInfoMap, _ := s.metaStore.GetFileInfoMap(ctx, empty)
	return &RaftInternalState{
		IsLeader: s.isLeader,
		Term:     s.term,
		Log:      s.log,
		MetaMap:  fileInfoMap,
	}, nil
}

var _ RaftSurfstoreInterface = new(RaftSurfstore)
