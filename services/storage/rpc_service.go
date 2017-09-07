package storage

import (
	"context"
	"math"

	"github.com/gogo/protobuf/types"
	"github.com/influxdata/influxdb/tsdb"
	"github.com/uber-go/zap"
)

//go:generate protoc -I$GOPATH/src -I. --plugin=protoc-gen-yarpc=$GOPATH/bin/protoc-gen-yarpc --yarpc_out=Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types:. --gogofaster_out=Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types:. storage.proto predicate.proto
//go:generate tmpl -data=@batch_cursor.gen.go.tmpldata batch_cursor.gen.go.tmpl

type rpcService struct {
	Store *Store

	Logger zap.Logger
}

func (r *rpcService) Capabilities(context.Context, *types.Empty) (*CapabilitiesResponse, error) {
	panic("implement me")
}

func (r *rpcService) Hints(context.Context, *types.Empty) (*HintsResponse, error) {
	panic("implement me")
}

func (r *rpcService) Read(req *ReadRequest, stream Storage_ReadServer) error {
	const BatchSize = 5000
	const FrameCount = 50

	r.Logger.Info("request",
		zap.String("predicate", PredicateToExprString(req.Predicate)),
		zap.Uint64("series_limit", req.SeriesLimit),
		zap.Uint64("series_offset", req.SeriesOffset),
		zap.Uint64("points_limit", req.PointsLimit),
		zap.Int64("start", req.TimestampRange.Start),
		zap.Int64("end", req.TimestampRange.End),
		zap.Bool("desc", req.Descending),
	)

	if req.PointsLimit == 0 {
		req.PointsLimit = math.MaxUint64
	}

	rs, err := r.Store.Read(req)
	if err != nil {
		r.Logger.Error("Store.Read failed", zap.Error(err))
		return err
	}

	if rs == nil {
		stream.Send(&ReadResponse{})
		return nil
	}

	b := 0
	var res ReadResponse
	res.Frames = make([]ReadResponse_Frame, 0, FrameCount)

	for rs.Next() {
		if len(res.Frames) >= FrameCount {
			// TODO(sgc): if last frame is a series, strip it
			err = stream.Send(&res)
			if err != nil {
				r.Logger.Error("stream.Send failed", zap.Error(err))
				rs.Close()
				break
			}
			res.Frames = make([]ReadResponse_Frame, 0, FrameCount)
		}

		cur := rs.Cursor()
		if cur == nil {
			// no data for series key + field combination
			continue
		}

		ss := len(res.Frames)

		next := rs.Tags()
		sf := ReadResponse_SeriesFrame{Name: rs.SeriesKey()}
		sf.Tags = make([]Tag, len(next))
		for i, t := range next {
			sf.Tags[i] = Tag(t)
		}
		res.Frames = append(res.Frames, ReadResponse_Frame{&ReadResponse_Frame_Series{&sf}})

		switch cur := cur.(type) {
		case tsdb.IntegerBatchCursor:
			frame := &ReadResponse_IntegerPointsFrame{Timestamps: make([]int64, 0, BatchSize), Values: make([]int64, 0, BatchSize)}
			res.Frames = append(res.Frames, ReadResponse_Frame{&ReadResponse_Frame_IntegerPoints{frame}})

			for {
				ts, vs := cur.Next()
				if len(ts) == 0 {
					break
				}

				frame.Timestamps = append(frame.Timestamps, ts...)
				frame.Values = append(frame.Values, vs...)

				b++
				if b >= BatchSize {
					frame = &ReadResponse_IntegerPointsFrame{Timestamps: make([]int64, 0, BatchSize), Values: make([]int64, 0, BatchSize)}
					res.Frames = append(res.Frames, ReadResponse_Frame{&ReadResponse_Frame_IntegerPoints{frame}})
					b = 0
				}
			}

		case tsdb.FloatBatchCursor:
			frame := &ReadResponse_FloatPointsFrame{Timestamps: make([]int64, 0, BatchSize), Values: make([]float64, 0, BatchSize)}
			res.Frames = append(res.Frames, ReadResponse_Frame{&ReadResponse_Frame_FloatPoints{frame}})

			for {
				ts, vs := cur.Next()
				if len(ts) == 0 {
					break
				}

				frame.Timestamps = append(frame.Timestamps, ts...)
				frame.Values = append(frame.Values, vs...)

				b++
				if b >= BatchSize {
					frame = &ReadResponse_FloatPointsFrame{Timestamps: make([]int64, 0, BatchSize), Values: make([]float64, 0, BatchSize)}
					res.Frames = append(res.Frames, ReadResponse_Frame{&ReadResponse_Frame_FloatPoints{frame}})
					b = 0
				}
			}

		default:

		}

		cur.Close()

		if len(res.Frames) == ss+1 {
			// no points collected, so strip series
			res.Frames = res.Frames[:ss]
		}
	}

	if len(res.Frames) > 0 {
		stream.Send(&res)
	}

	return nil
}
