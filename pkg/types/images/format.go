package images

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
(Format) String() string             String value
(Format) Title() string              title{}, golang name if not set
FormatValidIndexType(v Format) bool  Is value known
(Format) Valid() bool                Is value known
FormatValid(v string) bool           Is raw string a known value
FormatIndexes() []Format             All values
FormatOutputIndexes() []Format       Values with #output
(Format) IsOutput() bool             Has #output
FormatOutputList() []Format          Values with #output
(Format) Output() bool               Has #output
(Format) Mime() string               mime{string}
*/
type Format string

const (
	Avif = Format("avif") //@mime=image/avif #output
	Webp = Format("webp") //@mime=image/webp #output
	Png  = Format("png")  //@mime=image/png #output
	Jpg  = Format("jpg")  //@mime=image/jpeg #output
	Heic = Format("heic")
	Heif = Format("heif")
	Gif  = Format("gif")
	Tiff = Format("tiff")
)

func (a Format) String() string {
	switch a {
	case Avif:
		return "avif"
	case Webp:
		return "webp"
	case Png:
		return "png"
	case Jpg:
		return "jpg"
	case Heic:
		return "heic"
	case Heif:
		return "heif"
	case Gif:
		return "gif"
	case Tiff:
		return "tiff"
	default:
		return ""
	}
}

func (a Format) Title() string {
	switch a {
	case Avif:
		return "Avif"
	case Webp:
		return "Webp"
	case Png:
		return "Png"
	case Jpg:
		return "Jpg"
	case Heic:
		return "Heic"
	case Heif:
		return "Heif"
	case Gif:
		return "Gif"
	case Tiff:
		return "Tiff"
	default:
		return ""
	}
}

func FormatValidIndexType(v Format) bool {
	switch v {
	case Avif, Webp, Png, Jpg, Heic, Heif, Gif, Tiff:
		return true
	default:
		return false
	}
}

func (a Format) Valid() bool {
	return FormatValidIndexType(a)
}

func FormatValid(v string) bool {
	switch Format(v) {
	case Avif, Webp, Png, Jpg, Heic, Heif, Gif, Tiff:
		return true
	default:
		return false
	}
}

func FormatIndexes() []Format {
	return []Format{Avif, Webp, Png, Jpg, Heic, Heif, Gif, Tiff}
}

func FormatOutputIndexes() []Format {
	return []Format{Avif, Webp, Png, Jpg}
}

func (a Format) IsOutput() bool {
	switch a {
	case Avif, Webp, Png, Jpg:
		return true
	default:
		return false
	}
}

func FormatOutputList() []Format {
	return []Format{Avif, Webp, Png, Jpg}
}

func (a Format) Output() bool {
	switch a {
	case Avif, Webp, Png, Jpg:
		return true
	default:
		return false
	}
}

func (a Format) Mime() string {
	switch a {
	case Avif:
		return "image/avif"
	case Webp:
		return "image/webp"
	case Png:
		return "image/png"
	case Jpg:
		return "image/jpeg"
	default:
		return ""
	}
}
