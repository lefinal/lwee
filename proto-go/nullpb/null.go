package nullpb

import (
	"fmt"
	"github.com/gofrs/uuid"
	"github.com/lefinal/lwee/proto-go/nullpb/protonull"
	"github.com/lefinal/meh"
)

func UUIDFromProtoNullString(id *protonull.NullableID) (uuid.NullUUID, error) {
	switch protoID := id.GetKind().(type) {
	case *protonull.NullableID_Null:
		return uuid.NullUUID{}, nil
	case *protonull.NullableID_Data:
		parsed, err := uuid.FromString(protoID.Data)
		if err != nil {
			return uuid.NullUUID{
				UUID:  parsed,
				Valid: true,
			}, meh.NewBadInputErrFromErr(err, "parse uuid", meh.Details{"was": protoID.Data})
		}
	}
	return uuid.NullUUID{}, meh.NewInternalErr(fmt.Sprintf("unexpected type %T", id), meh.Details{"id": id})
}

func ProtoNullStringFromUUID(id uuid.NullUUID) *protonull.NullableID {
	if !id.Valid {
		return &protonull.NullableID{
			Kind: &protonull.NullableID_Null{},
		}
	}
	return &protonull.NullableID{
		Kind: &protonull.NullableID_Data{Data: id.UUID.String()},
	}
}
