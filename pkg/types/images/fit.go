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
(Fit) String() string          String value
(Fit) Title() string           title{}, golang name if not set
FitValidIndexType(v Fit) bool  Is value known
(Fit) Valid() bool             Is value known
FitValid(v string) bool        Is raw string a known value
FitIndexes() []Fit             All values
*/
type Fit string

const (
	Inside  = Fit("inside")  //Без обрезки, сохраняет пропорции
	Cover   = Fit("cover")   //Заполнение с обрезкой
	Contain = Fit("contain") //Вписывание с добавлением фона
	Fill    = Fit("fill")    //Растягивание
)

func (a Fit) String() string {
	switch a {
	case Inside:
		return "inside"
	case Cover:
		return "cover"
	case Contain:
		return "contain"
	case Fill:
		return "fill"
	default:
		return ""
	}
}

func (a Fit) Title() string {
	switch a {
	case Inside:
		return "Inside"
	case Cover:
		return "Cover"
	case Contain:
		return "Contain"
	case Fill:
		return "Fill"
	default:
		return ""
	}
}

func FitValidIndexType(v Fit) bool {
	switch v {
	case Inside, Cover, Contain, Fill:
		return true
	default:
		return false
	}
}

func (a Fit) Valid() bool {
	return FitValidIndexType(a)
}

func FitValid(v string) bool {
	switch Fit(v) {
	case Inside, Cover, Contain, Fill:
		return true
	default:
		return false
	}
}

func FitIndexes() []Fit {
	return []Fit{Inside, Cover, Contain, Fill}
}
