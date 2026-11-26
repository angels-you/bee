package code

/*
Constants Generator
Help to auto-generated correct constants for lists
Martin Prestone (c) 2024
github.com/monopolly

Struct options (first // line)
name=Status   Named type for constants: type Status int. Package funcs get type prefix: StatusIndex(), StatusValid(), so many lists can live in one package
type=string   Constants type: int (default), int8..int64, uint..uint64, string
#simple       Constants only, no functions
#up           Uppercase golang names: NEW instead of New
#index        Additional int constants StatusIndexNew, StatusIndexActive... (from 1)
#swift        Create .swift enum next to .go file

Field options (after // on field line)
any.string    Value can be any string: sensor.alarm.* > SensorAlarmAny, sensor.> > SensorAll, $SYS.REQ.PING > SysReqPing
"a b//c"      Quoted value if it has spaces or //
_             Skip value (reserves iota position)
iota=100      Start new const block from this value (int types)
name{}        Replace string value. Ex: art // name{artwork}
go{}          Replace golang const name. Ex: art // go{ArtList}
title{}       Title() for field. Ex: title{Real Estate}. Golang name if not set
desc{}        Desc() for field. Ex: desc{Asset is on sale}. Desc() is generated only if any field has desc{}
list{}        List() []string for field. Ex: list{geoid, country}
#tag          Group by tag. Ex: #virtual > IsVirtual() bool, Virtual() bool, StatusVirtualIndexes()
@tag          Same as #tag
key{int}      Custom func. Ex: code{401} > Code() int
key{text}     Custom func. Ex: label{Nice code} > Label() string
key{bool}     Custom func. Ex: paid{true} > Paid() bool
@key=value    Same as key{value}, value without spaces. Ex: @code=401

Custom funcs: key is capitalized (code{} > Code()), type is int if all values are numbers,
bool if all values are true/false, otherwise string. Missing values return 0 / "" / false.

Golang names: words split by any non letter/digit, * > Any, > > All, {id} > Id,
UPPER words kept (BEMT) unless whole value is upper ($SYS.REQ > SysReq), #up > SENSOR_ALARM_ANY.

Generated functions
(Code) String() string           String value
(Code) Title() string            title{}, golang name if not set
(Code) Desc() string             desc{}
CodeValidIndexType(v Code) bool  Is value known
(Code) Valid() bool              Is value known
CodeValid(v string) bool         Is raw string a known value
CodeIndexes() []Code             All values
(Code) Retryable() bool          retryable{bool}
(Code) Redeliver() bool          redeliver{bool}
*/
type Code string

const (
	DownloadTimeout        = Code("DOWNLOAD_TIMEOUT")         //@retryable=true @redeliver=true desc{a download by a url of the job (source, sound, watermark) did not finish within BEE_DOWNLOAD_TIMEOUT}
	SourceUrlExpired       = Code("SOURCE_URL_EXPIRED")       //@retryable=true @redeliver=false desc{a presigned url expired: the same job fails again, send a new job with a fresh url}
	SourceUnavailable      = Code("SOURCE_UNAVAILABLE")       //@retryable=true @redeliver=true desc{a download failed: network error, access denied or an unexpected http status}
	StorageTimeout         = Code("STORAGE_TIMEOUT")          //@retryable=true @redeliver=true desc{a write to the storage or the check of an output key timed out}
	StorageUnavailable     = Code("STORAGE_UNAVAILABLE")      //@retryable=true @redeliver=true desc{a write to the storage or the check of an output key failed}
	InternalTemporaryError = Code("INTERNAL_TEMPORARY_ERROR") //@retryable=true @redeliver=true desc{busy, cancelled, timed out or an unexpected failure of bee}
	InvalidJob             = Code("INVALID_JOB")              //@retryable=false @redeliver=false desc{the job is malformed: id, urls, options, profiles, tasks}
	InvalidPath            = Code("INVALID_PATH")             //@retryable=false @redeliver=false desc{an output key is empty, not a clean relative key, has the wrong extension or is used twice}
	InvalidDimensions      = Code("INVALID_DIMENSIONS")       //@retryable=false @redeliver=false desc{a size of the job is odd, negative, missing or above the limit of bee}
	InvalidQuality         = Code("INVALID_QUALITY")          //@retryable=false @redeliver=false desc{a quality is outside 0..100 or missing where it is required}
	UnsupportedCodec       = Code("UNSUPPORTED_CODEC")        //@retryable=false @redeliver=false desc{the job asks for a codec, pixel format, preset or image format bee does not encode}
	UnsupportedFormat      = Code("UNSUPPORTED_FORMAT")       //@retryable=false @redeliver=false desc{the container or codec of the source is one bee does not accept}
	InvalidAudio           = Code("INVALID_AUDIO")            //@retryable=false @redeliver=false desc{the audio is empty, corrupted, silent or has no audio stream}
	InvalidVideo           = Code("INVALID_VIDEO")            //@retryable=false @redeliver=false desc{the video is empty, corrupted, has no video stream or cannot be decoded}
	InvalidImage           = Code("INVALID_IMAGE")            //@retryable=false @redeliver=false desc{the image is empty, corrupted or cannot be decoded}
	InvalidSound           = Code("INVALID_SOUND")            //@retryable=false @redeliver=false desc{the external sound of a video job is empty, corrupted or has no audio stream}
	InvalidWatermark       = Code("INVALID_WATERMARK")        //@retryable=false @redeliver=false desc{the watermark is empty, corrupted or not a still image}
	SourceTooLarge         = Code("SOURCE_TOO_LARGE")         //@retryable=false @redeliver=false desc{a downloaded file is bigger than the limit of bee}
	DimensionLimitExceeded = Code("DIMENSION_LIMIT_EXCEEDED") //@retryable=false @redeliver=false desc{the source frame is larger than the limit of bee on a side}
	PixelLimitExceeded     = Code("PIXEL_LIMIT_EXCEEDED")     //@retryable=false @redeliver=false desc{the source image has more pixels than the limit of bee}
	DurationLimitExceeded  = Code("DURATION_LIMIT_EXCEEDED")  //@retryable=false @redeliver=false desc{the source or the processed timeline is shorter or longer than the limits of bee}
	NoRendition            = Code("NO_RENDITION")             //@retryable=false @redeliver=false desc{without upscale no profile fits the source: every rendition would be larger than the source}
	OutputTooLarge         = Code("OUTPUT_TOO_LARGE")         //@retryable=false @redeliver=false desc{an output stays above its max_bytes even at the lowest quality}
	EncodeFailed           = Code("ENCODE_FAILED")            //@retryable=false @redeliver=false desc{ffmpeg or vips failed on an accepted source}
	VerifyFailed           = Code("VERIFY_FAILED")            //@retryable=false @redeliver=false desc{an output did not pass the check: stream, size, duration or segments}
)

func (a Code) String() string {
	switch a {
	case DownloadTimeout:
		return "DOWNLOAD_TIMEOUT"
	case SourceUrlExpired:
		return "SOURCE_URL_EXPIRED"
	case SourceUnavailable:
		return "SOURCE_UNAVAILABLE"
	case StorageTimeout:
		return "STORAGE_TIMEOUT"
	case StorageUnavailable:
		return "STORAGE_UNAVAILABLE"
	case InternalTemporaryError:
		return "INTERNAL_TEMPORARY_ERROR"
	case InvalidJob:
		return "INVALID_JOB"
	case InvalidPath:
		return "INVALID_PATH"
	case InvalidDimensions:
		return "INVALID_DIMENSIONS"
	case InvalidQuality:
		return "INVALID_QUALITY"
	case UnsupportedCodec:
		return "UNSUPPORTED_CODEC"
	case UnsupportedFormat:
		return "UNSUPPORTED_FORMAT"
	case InvalidAudio:
		return "INVALID_AUDIO"
	case InvalidVideo:
		return "INVALID_VIDEO"
	case InvalidImage:
		return "INVALID_IMAGE"
	case InvalidSound:
		return "INVALID_SOUND"
	case InvalidWatermark:
		return "INVALID_WATERMARK"
	case SourceTooLarge:
		return "SOURCE_TOO_LARGE"
	case DimensionLimitExceeded:
		return "DIMENSION_LIMIT_EXCEEDED"
	case PixelLimitExceeded:
		return "PIXEL_LIMIT_EXCEEDED"
	case DurationLimitExceeded:
		return "DURATION_LIMIT_EXCEEDED"
	case NoRendition:
		return "NO_RENDITION"
	case OutputTooLarge:
		return "OUTPUT_TOO_LARGE"
	case EncodeFailed:
		return "ENCODE_FAILED"
	case VerifyFailed:
		return "VERIFY_FAILED"
	default:
		return ""
	}
}

func (a Code) Title() string {
	switch a {
	case DownloadTimeout:
		return "DownloadTimeout"
	case SourceUrlExpired:
		return "SourceUrlExpired"
	case SourceUnavailable:
		return "SourceUnavailable"
	case StorageTimeout:
		return "StorageTimeout"
	case StorageUnavailable:
		return "StorageUnavailable"
	case InternalTemporaryError:
		return "InternalTemporaryError"
	case InvalidJob:
		return "InvalidJob"
	case InvalidPath:
		return "InvalidPath"
	case InvalidDimensions:
		return "InvalidDimensions"
	case InvalidQuality:
		return "InvalidQuality"
	case UnsupportedCodec:
		return "UnsupportedCodec"
	case UnsupportedFormat:
		return "UnsupportedFormat"
	case InvalidAudio:
		return "InvalidAudio"
	case InvalidVideo:
		return "InvalidVideo"
	case InvalidImage:
		return "InvalidImage"
	case InvalidSound:
		return "InvalidSound"
	case InvalidWatermark:
		return "InvalidWatermark"
	case SourceTooLarge:
		return "SourceTooLarge"
	case DimensionLimitExceeded:
		return "DimensionLimitExceeded"
	case PixelLimitExceeded:
		return "PixelLimitExceeded"
	case DurationLimitExceeded:
		return "DurationLimitExceeded"
	case NoRendition:
		return "NoRendition"
	case OutputTooLarge:
		return "OutputTooLarge"
	case EncodeFailed:
		return "EncodeFailed"
	case VerifyFailed:
		return "VerifyFailed"
	default:
		return ""
	}
}

func (a Code) Desc() string {
	switch a {
	case DownloadTimeout:
		return "a download by a url of the job (source, sound, watermark) did not finish within BEE_DOWNLOAD_TIMEOUT"
	case SourceUrlExpired:
		return "a presigned url expired: the same job fails again, send a new job with a fresh url"
	case SourceUnavailable:
		return "a download failed: network error, access denied or an unexpected http status"
	case StorageTimeout:
		return "a write to the storage or the check of an output key timed out"
	case StorageUnavailable:
		return "a write to the storage or the check of an output key failed"
	case InternalTemporaryError:
		return "busy, cancelled, timed out or an unexpected failure of bee"
	case InvalidJob:
		return "the job is malformed: id, urls, options, profiles, tasks"
	case InvalidPath:
		return "an output key is empty, not a clean relative key, has the wrong extension or is used twice"
	case InvalidDimensions:
		return "a size of the job is odd, negative, missing or above the limit of bee"
	case InvalidQuality:
		return "a quality is outside 0..100 or missing where it is required"
	case UnsupportedCodec:
		return "the job asks for a codec, pixel format, preset or image format bee does not encode"
	case UnsupportedFormat:
		return "the container or codec of the source is one bee does not accept"
	case InvalidAudio:
		return "the audio is empty, corrupted, silent or has no audio stream"
	case InvalidVideo:
		return "the video is empty, corrupted, has no video stream or cannot be decoded"
	case InvalidImage:
		return "the image is empty, corrupted or cannot be decoded"
	case InvalidSound:
		return "the external sound of a video job is empty, corrupted or has no audio stream"
	case InvalidWatermark:
		return "the watermark is empty, corrupted or not a still image"
	case SourceTooLarge:
		return "a downloaded file is bigger than the limit of bee"
	case DimensionLimitExceeded:
		return "the source frame is larger than the limit of bee on a side"
	case PixelLimitExceeded:
		return "the source image has more pixels than the limit of bee"
	case DurationLimitExceeded:
		return "the source or the processed timeline is shorter or longer than the limits of bee"
	case NoRendition:
		return "without upscale no profile fits the source: every rendition would be larger than the source"
	case OutputTooLarge:
		return "an output stays above its max_bytes even at the lowest quality"
	case EncodeFailed:
		return "ffmpeg or vips failed on an accepted source"
	case VerifyFailed:
		return "an output did not pass the check: stream, size, duration or segments"
	default:
		return ""
	}
}

func CodeValidIndexType(v Code) bool {
	switch v {
	case DownloadTimeout, SourceUrlExpired, SourceUnavailable, StorageTimeout, StorageUnavailable, InternalTemporaryError, InvalidJob, InvalidPath, InvalidDimensions, InvalidQuality, UnsupportedCodec, UnsupportedFormat, InvalidAudio, InvalidVideo, InvalidImage, InvalidSound, InvalidWatermark, SourceTooLarge, DimensionLimitExceeded, PixelLimitExceeded, DurationLimitExceeded, NoRendition, OutputTooLarge, EncodeFailed, VerifyFailed:
		return true
	default:
		return false
	}
}

func (a Code) Valid() bool {
	return CodeValidIndexType(a)
}

func CodeValid(v string) bool {
	switch Code(v) {
	case DownloadTimeout, SourceUrlExpired, SourceUnavailable, StorageTimeout, StorageUnavailable, InternalTemporaryError, InvalidJob, InvalidPath, InvalidDimensions, InvalidQuality, UnsupportedCodec, UnsupportedFormat, InvalidAudio, InvalidVideo, InvalidImage, InvalidSound, InvalidWatermark, SourceTooLarge, DimensionLimitExceeded, PixelLimitExceeded, DurationLimitExceeded, NoRendition, OutputTooLarge, EncodeFailed, VerifyFailed:
		return true
	default:
		return false
	}
}

func CodeIndexes() []Code {
	return []Code{DownloadTimeout, SourceUrlExpired, SourceUnavailable, StorageTimeout, StorageUnavailable, InternalTemporaryError, InvalidJob, InvalidPath, InvalidDimensions, InvalidQuality, UnsupportedCodec, UnsupportedFormat, InvalidAudio, InvalidVideo, InvalidImage, InvalidSound, InvalidWatermark, SourceTooLarge, DimensionLimitExceeded, PixelLimitExceeded, DurationLimitExceeded, NoRendition, OutputTooLarge, EncodeFailed, VerifyFailed}
}

func (a Code) Retryable() bool {
	switch a {
	case DownloadTimeout, SourceUrlExpired, SourceUnavailable, StorageTimeout, StorageUnavailable, InternalTemporaryError:
		return true
	default:
		return false
	}
}

func (a Code) Redeliver() bool {
	switch a {
	case DownloadTimeout, SourceUnavailable, StorageTimeout, StorageUnavailable, InternalTemporaryError:
		return true
	default:
		return false
	}
}
