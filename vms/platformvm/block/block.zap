# The P-chain block wire.
#
# One ZAP object per block. Byte 0 is the kind discriminator; the rest of the
# fixed section is the same for every kind, so a reader dispatches on one byte
# and then reads at compile-time offsets. Nothing is length-prefixed and there
# is no version byte: the ZAP header carries the version and the object header
# carries the size.
#
#   kind 1 = abort, 2 = commit, 3 = proposal, 4 = standard
#
# A decision tx is stored twice-indirect: TxLengths is a u32 list of per-tx
# byte counts and TxBlob is the concatenation of each tx's own bytes, so the
# reader re-splits the blob and hands each slice to the tx parser without
# copying. ProposalTx holds the single proposal tx of a proposal block.
#
# Object sizes fall out of the offsets: 49 / 65 / 73.

package block

type id32 = bytes_fixed[32]

# abort and commit carry no transactions.
struct DecidedBlock {
    Kind     u8   @0
    ParentID id32 @1
    Height   u64  @33
    Time     u64  @41
}

struct StandardBlock {
    Kind      u8        @0
    ParentID  id32      @1
    Height    u64       @33
    Time      u64       @41
    TxLengths list<u32> @49
    TxBlob    bytes     @57
}

struct ProposalBlock {
    Kind       u8        @0
    ParentID   id32      @1
    Height     u64       @33
    Time       u64       @41
    TxLengths  list<u32> @49
    TxBlob     bytes     @57
    ProposalTx bytes     @65
}
