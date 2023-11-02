package errorpb

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lefinal/lwee/proto-go/errorpb/protoerror"
	"github.com/lefinal/meh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
)

func Pack(err error) error {
	code := codes.Unknown
	switch meh.ErrorCode(err) {
	case meh.ErrBadInput:
		code = codes.InvalidArgument
	case meh.ErrNotFound:
		code = codes.NotFound
	case meh.ErrForbidden:
		code = codes.PermissionDenied
	case meh.ErrUnauthorized:
		code = codes.Unauthenticated
	case meh.ErrInternal:
		code = codes.Internal
	case meh.ErrUnexpected,
		meh.ErrNeutral:
		code = codes.Unknown
	}
	grpcErrStatus := status.New(code, err.Error())
	// Add original meh-error.
	mehErr := meh.Cast(err)
	mehErrRaw, err := json.Marshal(mehErr)
	if err != nil {
		// Maybe log?
	} else {
		grpcErrStatus, err = grpcErrStatus.WithDetails(&protoerror.Error{MehError: &anypb.Any{Value: mehErrRaw}})
	}
	return grpcErrStatus.Err()
}

func Unpack(errToUnpack error) error {
	statusErr, ok := status.FromError(errToUnpack)
	if !ok {
		return meh.NewInternalErr(fmt.Sprintf("error to unpack was no status error but %T", errToUnpack), meh.Details{"err_to_unpack": errToUnpack})
	}
	for _, detail := range statusErr.Details() {
		errMessage, ok := detail.(*protoerror.Error)
		if !ok {
			continue
		}
		var mehErr meh.Error
		err := json.Unmarshal(errMessage.MehError.Value, &mehErr)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "unmarshal packed meh error", meh.Details{"value_was": string(errMessage.MehError.Value)})
		}
		return &mehErr
	}
	return meh.NewInternalErrFromErr(errToUnpack, "native grpc", meh.Details{"is_meh": false})
}

func Interceptors() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.UnaryInterceptor(errorInterceptor()),
	}
}

func errorInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Call the invoker to execute RPC.
		response, err := handler(ctx, req)
		if err != nil {
			err = Pack(err)
		}
		return response, err
	}
}
