module generator-test

go 1.24rc2

toolchain go1.24.2

replace github.com/refaktor/ryegen/v2 => ../../

tool github.com/refaktor/ryegen/v2

require (
	fyne.io/fyne/v2 v2.6.1
	fyne.io/systray v1.11.0
	github.com/akavel/rsrc v0.10.2
	github.com/fsnotify/fsnotify v1.9.0
	github.com/fyne-io/oksvg v0.1.0
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20250301202403-da16c1255728
	github.com/go-text/typesetting v0.3.0
	github.com/godbus/dbus/v5 v5.1.0
	github.com/josephspurrier/goversioninfo v1.4.0
	github.com/lucor/goinfo v0.9.0
	github.com/nicksnyder/go-i18n/v2 v2.6.0
	github.com/refaktor/rye v0.0.82-0.20250429085106-5940683e659e
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	github.com/urfave/cli/v2 v2.4.0
	github.com/yuin/goldmark v1.7.11
	golang.org/x/crypto v0.37.0
	golang.org/x/image v0.26.0
	golang.org/x/mod v0.24.0
	golang.org/x/net v0.39.0
	golang.org/x/sys v0.33.0
	golang.org/x/term v0.31.0
	golang.org/x/text v0.24.0
	golang.org/x/tools v0.32.0
)

require (
	filippo.io/age v1.2.1 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/JohannesKaufmann/dom v0.2.0 // indirect
	github.com/JohannesKaufmann/html-to-markdown/v2 v2.3.2 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.4.5 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aws/aws-sdk-go-v2 v1.36.3 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.10 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.29.14 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.67 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.7.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.79.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ses v1.30.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.30.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.19 // indirect
	github.com/aws/smithy-go v1.22.3 // indirect
	github.com/bitfield/script v0.24.1 // indirect
	github.com/bits-and-blooms/bitset v1.22.0 // indirect
	github.com/blevesearch/bleve/v2 v2.5.0 // indirect
	github.com/blevesearch/bleve_index_api v1.2.8 // indirect
	github.com/blevesearch/geo v0.2.0 // indirect
	github.com/blevesearch/go-faiss v1.0.25 // indirect
	github.com/blevesearch/go-porterstemmer v1.0.3 // indirect
	github.com/blevesearch/gtreap v0.1.1 // indirect
	github.com/blevesearch/mmap-go v1.0.4 // indirect
	github.com/blevesearch/scorch_segment_api/v2 v2.3.10 // indirect
	github.com/blevesearch/segment v0.9.1 // indirect
	github.com/blevesearch/snowballstem v0.9.0 // indirect
	github.com/blevesearch/upsidedown_store_api v1.0.2 // indirect
	github.com/blevesearch/vellum v1.1.0 // indirect
	github.com/blevesearch/zapx/v11 v11.4.1 // indirect
	github.com/blevesearch/zapx/v12 v12.4.1 // indirect
	github.com/blevesearch/zapx/v13 v13.4.1 // indirect
	github.com/blevesearch/zapx/v14 v14.4.1 // indirect
	github.com/blevesearch/zapx/v15 v15.4.1 // indirect
	github.com/blevesearch/zapx/v16 v16.2.3 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/drewlanenga/govector v0.0.0-20220726163947-b958ac08bc93 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/elastic/go-seccomp-bpf v1.5.0 // indirect
	github.com/fogleman/gg v1.3.0 // indirect
	github.com/fredbi/uri v1.1.0 // indirect
	github.com/fyne-io/gl-js v0.1.0 // indirect
	github.com/fyne-io/glfw-js v0.2.0 // indirect
	github.com/fyne-io/image v0.1.1 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/gdamore/tcell/v2 v2.8.1 // indirect
	github.com/glebarez/go-sqlite v1.22.0 // indirect
	github.com/glebarez/sqlite v1.11.0 // indirect
	github.com/gliderlabs/ssh v0.3.8 // indirect
	github.com/go-gomail/gomail v0.0.0-20160411212932-81ebce5c23df // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-sql-driver/mysql v1.9.2 // indirect
	github.com/go-telegram-bot-api/telegram-bot-api v4.6.4+incompatible // indirect
	github.com/go-text/render v0.2.0 // indirect
	github.com/go-text/typesetting-utils v0.0.0-20241103174707-87a29e9e6066 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/gorilla/sessions v1.4.0 // indirect
	github.com/hack-pad/go-indexeddb v0.3.2 // indirect
	github.com/hack-pad/safejs v0.1.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/itchyny/gojq v0.12.17 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	github.com/jackmordaunt/icns/v2 v2.2.6 // indirect
	github.com/jeandeaual/go-locale v0.0.0-20241217141322-fcc2cadd6f08 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/jlaffaye/ftp v0.2.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jsummers/gobmp v0.0.0-20230614200233-a9de23ed2e25 // indirect
	github.com/kopoli/go-terminal-size v0.0.0-20170219200355-5c97524c8b54 // indirect
	github.com/labstack/echo v3.3.10+incompatible // indirect
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/landlock-lsm/go-landlock v0.0.0-20250303204525-1544bccde3a3 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20250317134145-8bc96cf8fc35 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mcuadros/go-version v0.0.0-20190830083331-035f6764e8d2 // indirect
	github.com/mhale/smtpd v0.8.3 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/mrz1836/postmark v1.7.3 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/natefinch/atomic v1.0.1 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/pkg/term v1.2.0-beta.2.0.20211217091447-1a4a3b719465 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/refaktor/go-peg v0.0.0-20250317092442-5acc59396305 // indirect
	github.com/refaktor/keyboard v0.0.0-20250327232248-edb0b31909c4 // indirect
	github.com/refaktor/ryegen/v2 v2.0.0-00010101000000-000000000000 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/richardlehane/mscfb v1.0.4 // indirect
	github.com/richardlehane/msoleps v1.0.4 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/rymdport/portal v0.4.1 // indirect
	github.com/sashabaranov/go-openai v1.39.0 // indirect
	github.com/shirou/gopsutil/v3 v3.24.5 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	github.com/technoweenie/multipartstreamer v1.0.1 // indirect
	github.com/thomasberger/parsemail v1.2.7 // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/tklauser/numcpus v0.10.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/xuri/efp v0.0.0-20250227110027-3491fafc2b79 // indirect
	github.com/xuri/excelize/v2 v2.9.0 // indirect
	github.com/xuri/nfp v0.0.0-20250226145837-86d5fc24b2ba // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.etcd.io/bbolt v1.4.0 // indirect
	go.mongodb.org/mongo-driver v1.17.3 // indirect
	golang.org/x/exp v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/tools/go/vcs v0.1.0-deprecated // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/gorm v1.26.0 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.76 // indirect
	modernc.org/libc v1.64.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.10.0 // indirect
	modernc.org/sqlite v1.37.0 // indirect
	mvdan.cc/sh/v3 v3.11.0 // indirect
	software.sslmate.com/src/go-pkcs12 v0.5.0 // indirect
)
