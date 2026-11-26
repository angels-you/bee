package provider

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

Generated functions
(Provider) String() string               String value
(Provider) Title() string                title{}, golang name if not set
(Provider) Int() int                     Value as int
ProviderValidIndexType(v Provider) bool  Is value known
(Provider) Valid() bool                  Is value known
ProviderValid(v int) bool                Is raw int a known value
ProviderIndexes() []Provider             All values
ProviderHostedIndexes() []Provider       Values with #hosted
(Provider) IsHosted() bool               Has #hosted
ProviderIndex(v string) int              String value to int, 0 if unknown
ProviderHostedList() []Provider          Values with #hosted
(Provider) Hosted() bool                 Has #hosted
(Provider) Country() string              country{string}
(Provider) Link() string                 link{string}
(Provider) Region() string               region{string}
(Provider) Regions() string              regions{string}
(Provider) Endpoint() string             endpoint{string}
(Provider) Egress() string               egress{string}
*/
type Provider int

const (
	Hetzner      = Provider(iota) + 1 //iota=1 country{de} link{hetzner.de} title{Hetzner S3} region{hel1} regions{hel1,fsn1,nbg1} endpoint{<region>.your-objectstorage.com}
	Selectel                          //country{ru} link{selectel.ru} title{Selectel S3} region{ru-1} regions{ru-1} endpoint{s3.storage.selcloud.ru}
	Cloudflare                        //country{us} link{cloudflare.com} title{Cloudflare R2} region{auto} regions{auto} endpoint{<account>.r2.cloudflarestorage.com} egress{free}
	Backblaze                         //country{us} link{backblaze.com} title{Backblaze B2} region{us-west-004} regions{us-west-001,us-west-002,us-west-004,us-east-005,eu-central-003} endpoint{s3.<region>.backblazeb2.com} egress{free via cloudflare}
	AWS                               //go{AWS} country{us} link{aws.amazon.com} title{Amazon S3} region{us-east-1} regions{us-east-1,us-west-2,eu-central-1,eu-west-1,ap-southeast-1} endpoint{s3.<region>.amazonaws.com}
	GCS                               //go{GCS} country{us} link{cloud.google.com} title{Google Cloud Storage} region{auto} regions{auto} endpoint{storage.googleapis.com}
	Wasabi                            //country{us} link{wasabi.com} title{Wasabi} region{us-east-1} regions{us-east-1,us-east-2,us-west-1,eu-central-1,eu-central-2,ap-northeast-1} endpoint{s3.<region>.wasabisys.com} egress{free fair use}
	DigitalOcean                      //go{DigitalOcean} country{us} link{digitalocean.com} title{DigitalOcean Spaces} region{nyc3} regions{nyc3,sfo3,ams3,fra1,sgp1,syd1} endpoint{<region>.digitaloceanspaces.com}
	Linode                            //country{us} link{linode.com} title{Akamai Linode Object Storage} region{us-east-1} regions{us-east-1,us-southeast-1,eu-central-1,ap-south-1} endpoint{<region>.linodeobjects.com}
	Vultr                             //country{us} link{vultr.com} title{Vultr Object Storage} region{ewr1} regions{ewr1,sjc1,ams1,blr1,del1,sgp1} endpoint{<region>.vultrobjects.com}
	Scaleway                          //country{fr} link{scaleway.com} title{Scaleway Object Storage} region{fr-par} regions{fr-par,nl-ams,pl-waw} endpoint{s3.<region>.scw.cloud}
	OVH                               //go{OVH} country{fr} link{ovhcloud.com} title{OVHcloud Object Storage} region{gra} regions{gra,sbg,rbx,de,uk,waw,bhs} endpoint{s3.<region>.io.cloud.ovh.net}
	UpCloud                           //go{UpCloud} country{fi} link{upcloud.com} title{UpCloud Object Storage} region{europe-1} endpoint{<account>.upcloudobjects.com}
	Exoscale                          //country{ch} link{exoscale.com} title{Exoscale SOS} region{ch-gva-2} regions{ch-gva-2,ch-dk-2,de-fra-1,de-muc-1,at-vie-1,at-vie-2,bg-sof-1} endpoint{sos-<region>.exo.io}
	Contabo                           //country{de} link{contabo.com} title{Contabo Object Storage} region{eu2} regions{eu2,usc1,sin1} endpoint{<region>.contabostorage.com} egress{free}
	IONOS                             //go{IONOS} country{de} link{ionos.com} title{IONOS S3 Object Storage} region{eu-central-1} regions{eu-central-1,eu-central-2,eu-south-2} endpoint{s3.<region>.ionoscloud.com}
	IBM                               //go{IBM} country{us} link{ibm.com} title{IBM Cloud Object Storage} region{us-south} regions{us-south,us-east,eu-de,eu-gb,jp-tok} endpoint{s3.<region>.cloud-object-storage.appdomain.cloud}
	Oracle                            //country{us} link{oracle.com} title{Oracle Cloud Object Storage} region{us-ashburn-1} regions{us-ashburn-1,us-phoenix-1,eu-frankfurt-1,uk-london-1} endpoint{<account>.compat.objectstorage.<region>.oraclecloud.com}
	Alibaba                           //country{cn} link{alibabacloud.com} title{Alibaba Cloud OSS} region{oss-cn-hangzhou} regions{oss-cn-hangzhou,oss-cn-beijing,oss-cn-shanghai,oss-ap-southeast-1,oss-eu-central-1} endpoint{<region>.aliyuncs.com}
	Tencent                           //country{cn} link{tencentcloud.com} title{Tencent Cloud COS} region{ap-guangzhou} regions{ap-guangzhou,ap-beijing,ap-shanghai,ap-singapore,eu-frankfurt} endpoint{cos.<region>.myqcloud.com}
	Huawei                            //country{cn} link{huaweicloud.com} title{Huawei Cloud OBS} region{cn-north-4} regions{cn-north-4,cn-east-3,ap-southeast-1,eu-west-101} endpoint{obs.<region>.myhuaweicloud.com}
	Yandex                            //country{ru} link{yandex.cloud} title{Yandex Object Storage} region{ru-central1} regions{ru-central1} endpoint{storage.yandexcloud.net}
	VK                                //go{VK} country{ru} link{cloud.vk.com} title{VK Cloud Object Storage} region{ru-msk} regions{ru-msk,kz-ast} endpoint{hb.<region>.vkcloud-storage.ru}
	Timeweb                           //country{ru} link{timeweb.cloud} title{Timeweb Cloud S3} region{ru-1} regions{ru-1} endpoint{s3.timeweb.cloud}
	Storj                             //country{us} link{storj.io} title{Storj} region{us1} regions{us1,eu1,ap1} endpoint{gateway.storjshare.io}
	IDrive                            //go{IDrive} country{us} link{idrive.com} title{IDrive e2} egress{free}
	Filebase                          //country{us} link{filebase.com} title{Filebase} region{us-east-1} regions{us-east-1} endpoint{s3.filebase.com}
	Tebi                              //country{ee} link{tebi.io} title{Tebi} region{global} regions{global} endpoint{s3.tebi.io}
	MinIO                             //go{MinIO} country{us} link{min.io} title{MinIO} region{us-east-1} endpoint{localhost:9000} #hosted
	Ceph                              //country{us} link{ceph.io} title{Ceph RGW} region{default} #hosted
)

func (a Provider) String() string {
	switch a {
	case Hetzner:
		return "hetzner"
	case Selectel:
		return "selectel"
	case Cloudflare:
		return "cloudflare"
	case Backblaze:
		return "backblaze"
	case AWS:
		return "aws"
	case GCS:
		return "gcs"
	case Wasabi:
		return "wasabi"
	case DigitalOcean:
		return "digitalocean"
	case Linode:
		return "linode"
	case Vultr:
		return "vultr"
	case Scaleway:
		return "scaleway"
	case OVH:
		return "ovh"
	case UpCloud:
		return "upcloud"
	case Exoscale:
		return "exoscale"
	case Contabo:
		return "contabo"
	case IONOS:
		return "ionos"
	case IBM:
		return "ibm"
	case Oracle:
		return "oracle"
	case Alibaba:
		return "alibaba"
	case Tencent:
		return "tencent"
	case Huawei:
		return "huawei"
	case Yandex:
		return "yandex"
	case VK:
		return "vk"
	case Timeweb:
		return "timeweb"
	case Storj:
		return "storj"
	case IDrive:
		return "idrive"
	case Filebase:
		return "filebase"
	case Tebi:
		return "tebi"
	case MinIO:
		return "minio"
	case Ceph:
		return "ceph"
	default:
		return ""
	}
}

func (a Provider) Title() string {
	switch a {
	case Hetzner:
		return "Hetzner S3"
	case Selectel:
		return "Selectel S3"
	case Cloudflare:
		return "Cloudflare R2"
	case Backblaze:
		return "Backblaze B2"
	case AWS:
		return "Amazon S3"
	case GCS:
		return "Google Cloud Storage"
	case Wasabi:
		return "Wasabi"
	case DigitalOcean:
		return "DigitalOcean Spaces"
	case Linode:
		return "Akamai Linode Object Storage"
	case Vultr:
		return "Vultr Object Storage"
	case Scaleway:
		return "Scaleway Object Storage"
	case OVH:
		return "OVHcloud Object Storage"
	case UpCloud:
		return "UpCloud Object Storage"
	case Exoscale:
		return "Exoscale SOS"
	case Contabo:
		return "Contabo Object Storage"
	case IONOS:
		return "IONOS S3 Object Storage"
	case IBM:
		return "IBM Cloud Object Storage"
	case Oracle:
		return "Oracle Cloud Object Storage"
	case Alibaba:
		return "Alibaba Cloud OSS"
	case Tencent:
		return "Tencent Cloud COS"
	case Huawei:
		return "Huawei Cloud OBS"
	case Yandex:
		return "Yandex Object Storage"
	case VK:
		return "VK Cloud Object Storage"
	case Timeweb:
		return "Timeweb Cloud S3"
	case Storj:
		return "Storj"
	case IDrive:
		return "IDrive e2"
	case Filebase:
		return "Filebase"
	case Tebi:
		return "Tebi"
	case MinIO:
		return "MinIO"
	case Ceph:
		return "Ceph RGW"
	default:
		return ""
	}
}

func (a Provider) Int() int {
	return int(a)
}

func ProviderValidIndexType(v Provider) bool {
	switch v {
	case Hetzner, Selectel, Cloudflare, Backblaze, AWS, GCS, Wasabi, DigitalOcean, Linode, Vultr, Scaleway, OVH, UpCloud, Exoscale, Contabo, IONOS, IBM, Oracle, Alibaba, Tencent, Huawei, Yandex, VK, Timeweb, Storj, IDrive, Filebase, Tebi, MinIO, Ceph:
		return true
	default:
		return false
	}
}

func (a Provider) Valid() bool {
	return ProviderValidIndexType(a)
}

func ProviderValid(v int) bool {
	switch Provider(v) {
	case Hetzner, Selectel, Cloudflare, Backblaze, AWS, GCS, Wasabi, DigitalOcean, Linode, Vultr, Scaleway, OVH, UpCloud, Exoscale, Contabo, IONOS, IBM, Oracle, Alibaba, Tencent, Huawei, Yandex, VK, Timeweb, Storj, IDrive, Filebase, Tebi, MinIO, Ceph:
		return true
	default:
		return false
	}
}

func ProviderIndexes() []Provider {
	return []Provider{Hetzner, Selectel, Cloudflare, Backblaze, AWS, GCS, Wasabi, DigitalOcean, Linode, Vultr, Scaleway, OVH, UpCloud, Exoscale, Contabo, IONOS, IBM, Oracle, Alibaba, Tencent, Huawei, Yandex, VK, Timeweb, Storj, IDrive, Filebase, Tebi, MinIO, Ceph}
}

func ProviderHostedIndexes() []Provider {
	return []Provider{MinIO, Ceph}
}

func (a Provider) IsHosted() bool {
	switch a {
	case MinIO, Ceph:
		return true
	default:
		return false
	}
}

func ProviderIndex(v string) int {
	switch v {
	case "hetzner":
		return int(Hetzner)
	case "selectel":
		return int(Selectel)
	case "cloudflare":
		return int(Cloudflare)
	case "backblaze":
		return int(Backblaze)
	case "aws":
		return int(AWS)
	case "gcs":
		return int(GCS)
	case "wasabi":
		return int(Wasabi)
	case "digitalocean":
		return int(DigitalOcean)
	case "linode":
		return int(Linode)
	case "vultr":
		return int(Vultr)
	case "scaleway":
		return int(Scaleway)
	case "ovh":
		return int(OVH)
	case "upcloud":
		return int(UpCloud)
	case "exoscale":
		return int(Exoscale)
	case "contabo":
		return int(Contabo)
	case "ionos":
		return int(IONOS)
	case "ibm":
		return int(IBM)
	case "oracle":
		return int(Oracle)
	case "alibaba":
		return int(Alibaba)
	case "tencent":
		return int(Tencent)
	case "huawei":
		return int(Huawei)
	case "yandex":
		return int(Yandex)
	case "vk":
		return int(VK)
	case "timeweb":
		return int(Timeweb)
	case "storj":
		return int(Storj)
	case "idrive":
		return int(IDrive)
	case "filebase":
		return int(Filebase)
	case "tebi":
		return int(Tebi)
	case "minio":
		return int(MinIO)
	case "ceph":
		return int(Ceph)
	default:
		return 0
	}
}

func ProviderHostedList() []Provider {
	return []Provider{MinIO, Ceph}
}

func (a Provider) Hosted() bool {
	switch a {
	case MinIO, Ceph:
		return true
	default:
		return false
	}
}

func (a Provider) Country() string {
	switch a {
	case Hetzner:
		return "de"
	case Selectel:
		return "ru"
	case Cloudflare:
		return "us"
	case Backblaze:
		return "us"
	case AWS:
		return "us"
	case GCS:
		return "us"
	case Wasabi:
		return "us"
	case DigitalOcean:
		return "us"
	case Linode:
		return "us"
	case Vultr:
		return "us"
	case Scaleway:
		return "fr"
	case OVH:
		return "fr"
	case UpCloud:
		return "fi"
	case Exoscale:
		return "ch"
	case Contabo:
		return "de"
	case IONOS:
		return "de"
	case IBM:
		return "us"
	case Oracle:
		return "us"
	case Alibaba:
		return "cn"
	case Tencent:
		return "cn"
	case Huawei:
		return "cn"
	case Yandex:
		return "ru"
	case VK:
		return "ru"
	case Timeweb:
		return "ru"
	case Storj:
		return "us"
	case IDrive:
		return "us"
	case Filebase:
		return "us"
	case Tebi:
		return "ee"
	case MinIO:
		return "us"
	case Ceph:
		return "us"
	default:
		return ""
	}
}

func (a Provider) Link() string {
	switch a {
	case Hetzner:
		return "hetzner.de"
	case Selectel:
		return "selectel.ru"
	case Cloudflare:
		return "cloudflare.com"
	case Backblaze:
		return "backblaze.com"
	case AWS:
		return "aws.amazon.com"
	case GCS:
		return "cloud.google.com"
	case Wasabi:
		return "wasabi.com"
	case DigitalOcean:
		return "digitalocean.com"
	case Linode:
		return "linode.com"
	case Vultr:
		return "vultr.com"
	case Scaleway:
		return "scaleway.com"
	case OVH:
		return "ovhcloud.com"
	case UpCloud:
		return "upcloud.com"
	case Exoscale:
		return "exoscale.com"
	case Contabo:
		return "contabo.com"
	case IONOS:
		return "ionos.com"
	case IBM:
		return "ibm.com"
	case Oracle:
		return "oracle.com"
	case Alibaba:
		return "alibabacloud.com"
	case Tencent:
		return "tencentcloud.com"
	case Huawei:
		return "huaweicloud.com"
	case Yandex:
		return "yandex.cloud"
	case VK:
		return "cloud.vk.com"
	case Timeweb:
		return "timeweb.cloud"
	case Storj:
		return "storj.io"
	case IDrive:
		return "idrive.com"
	case Filebase:
		return "filebase.com"
	case Tebi:
		return "tebi.io"
	case MinIO:
		return "min.io"
	case Ceph:
		return "ceph.io"
	default:
		return ""
	}
}

func (a Provider) Region() string {
	switch a {
	case Hetzner:
		return "hel1"
	case Selectel:
		return "ru-1"
	case Cloudflare:
		return "auto"
	case Backblaze:
		return "us-west-004"
	case AWS:
		return "us-east-1"
	case GCS:
		return "auto"
	case Wasabi:
		return "us-east-1"
	case DigitalOcean:
		return "nyc3"
	case Linode:
		return "us-east-1"
	case Vultr:
		return "ewr1"
	case Scaleway:
		return "fr-par"
	case OVH:
		return "gra"
	case UpCloud:
		return "europe-1"
	case Exoscale:
		return "ch-gva-2"
	case Contabo:
		return "eu2"
	case IONOS:
		return "eu-central-1"
	case IBM:
		return "us-south"
	case Oracle:
		return "us-ashburn-1"
	case Alibaba:
		return "oss-cn-hangzhou"
	case Tencent:
		return "ap-guangzhou"
	case Huawei:
		return "cn-north-4"
	case Yandex:
		return "ru-central1"
	case VK:
		return "ru-msk"
	case Timeweb:
		return "ru-1"
	case Storj:
		return "us1"
	case Filebase:
		return "us-east-1"
	case Tebi:
		return "global"
	case MinIO:
		return "us-east-1"
	case Ceph:
		return "default"
	default:
		return ""
	}
}

func (a Provider) Regions() string {
	switch a {
	case Hetzner:
		return "hel1,fsn1,nbg1"
	case Selectel:
		return "ru-1"
	case Cloudflare:
		return "auto"
	case Backblaze:
		return "us-west-001,us-west-002,us-west-004,us-east-005,eu-central-003"
	case AWS:
		return "us-east-1,us-west-2,eu-central-1,eu-west-1,ap-southeast-1"
	case GCS:
		return "auto"
	case Wasabi:
		return "us-east-1,us-east-2,us-west-1,eu-central-1,eu-central-2,ap-northeast-1"
	case DigitalOcean:
		return "nyc3,sfo3,ams3,fra1,sgp1,syd1"
	case Linode:
		return "us-east-1,us-southeast-1,eu-central-1,ap-south-1"
	case Vultr:
		return "ewr1,sjc1,ams1,blr1,del1,sgp1"
	case Scaleway:
		return "fr-par,nl-ams,pl-waw"
	case OVH:
		return "gra,sbg,rbx,de,uk,waw,bhs"
	case Exoscale:
		return "ch-gva-2,ch-dk-2,de-fra-1,de-muc-1,at-vie-1,at-vie-2,bg-sof-1"
	case Contabo:
		return "eu2,usc1,sin1"
	case IONOS:
		return "eu-central-1,eu-central-2,eu-south-2"
	case IBM:
		return "us-south,us-east,eu-de,eu-gb,jp-tok"
	case Oracle:
		return "us-ashburn-1,us-phoenix-1,eu-frankfurt-1,uk-london-1"
	case Alibaba:
		return "oss-cn-hangzhou,oss-cn-beijing,oss-cn-shanghai,oss-ap-southeast-1,oss-eu-central-1"
	case Tencent:
		return "ap-guangzhou,ap-beijing,ap-shanghai,ap-singapore,eu-frankfurt"
	case Huawei:
		return "cn-north-4,cn-east-3,ap-southeast-1,eu-west-101"
	case Yandex:
		return "ru-central1"
	case VK:
		return "ru-msk,kz-ast"
	case Timeweb:
		return "ru-1"
	case Storj:
		return "us1,eu1,ap1"
	case Filebase:
		return "us-east-1"
	case Tebi:
		return "global"
	default:
		return ""
	}
}

func (a Provider) Endpoint() string {
	switch a {
	case Hetzner:
		return "<region>.your-objectstorage.com"
	case Selectel:
		return "s3.storage.selcloud.ru"
	case Cloudflare:
		return "<account>.r2.cloudflarestorage.com"
	case Backblaze:
		return "s3.<region>.backblazeb2.com"
	case AWS:
		return "s3.<region>.amazonaws.com"
	case GCS:
		return "storage.googleapis.com"
	case Wasabi:
		return "s3.<region>.wasabisys.com"
	case DigitalOcean:
		return "<region>.digitaloceanspaces.com"
	case Linode:
		return "<region>.linodeobjects.com"
	case Vultr:
		return "<region>.vultrobjects.com"
	case Scaleway:
		return "s3.<region>.scw.cloud"
	case OVH:
		return "s3.<region>.io.cloud.ovh.net"
	case UpCloud:
		return "<account>.upcloudobjects.com"
	case Exoscale:
		return "sos-<region>.exo.io"
	case Contabo:
		return "<region>.contabostorage.com"
	case IONOS:
		return "s3.<region>.ionoscloud.com"
	case IBM:
		return "s3.<region>.cloud-object-storage.appdomain.cloud"
	case Oracle:
		return "<account>.compat.objectstorage.<region>.oraclecloud.com"
	case Alibaba:
		return "<region>.aliyuncs.com"
	case Tencent:
		return "cos.<region>.myqcloud.com"
	case Huawei:
		return "obs.<region>.myhuaweicloud.com"
	case Yandex:
		return "storage.yandexcloud.net"
	case VK:
		return "hb.<region>.vkcloud-storage.ru"
	case Timeweb:
		return "s3.timeweb.cloud"
	case Storj:
		return "gateway.storjshare.io"
	case Filebase:
		return "s3.filebase.com"
	case Tebi:
		return "s3.tebi.io"
	case MinIO:
		return "localhost:9000"
	default:
		return ""
	}
}

func (a Provider) Egress() string {
	switch a {
	case Cloudflare:
		return "free"
	case Backblaze:
		return "free via cloudflare"
	case Wasabi:
		return "free fair use"
	case Contabo:
		return "free"
	case IDrive:
		return "free"
	default:
		return ""
	}
}
