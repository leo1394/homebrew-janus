package store

import (
    "crypto/rand"
    "crypto/sha256"
    "fmt"
    "errors"
    "strings"
    "regexp"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)

var accountIDPattern=regexp.MustCompile(`^(janusacc-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|janusacc-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|acc0[0-9a-f]{28}|acc0[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|account:[0-9a-f]{32})$`)
var legacyAccountIDPattern=regexp.MustCompile(`^account:[0-9a-f]{32}$`)

// Normalize public IDs without changing the selected Keychain item.
func CanonicalAccountID(id string)string{
    if legacyAccountIDPattern.MatchString(id){sum:=sha256.Sum256([]byte(id));id=fmt.Sprintf("acc0%x",sum[:14])}
    if len(id)==32&&strings.HasPrefix(id,"acc0")&&accountIDPattern.MatchString(id){id=id[:8]+"-"+id[8:12]+"-"+id[12:16]+"-"+id[16:20]+"-"+id[20:]}
    if len(id)==36&&strings.HasPrefix(id,"acc0")&&accountIDPattern.MatchString(id){id="janusacc-"+id}
    if len(id)==45&&strings.HasPrefix(id,"janusacc-")&&accountIDPattern.MatchString(id){return "janusacc-"+id[18:]};return id
}
func normalizeAccounts(doc *document){
    for i:=range doc.Accounts{a:=&doc.Accounts[i];if a.CredentialID==""{a.CredentialID=a.ID};a.ID=CanonicalAccountID(a.ID)}
    for i:=range doc.Profiles{doc.Profiles[i].AccountID=CanonicalAccountID(doc.Profiles[i].AccountID)}
}
func identityKey(id string)string{parts:=strings.SplitN(strings.TrimPrefix(id,"janusacc-"),":",2);return strings.ReplaceAll(parts[len(parts)-1],"-","")}

func sameCredential(a,b janus.Profile)bool{
    if a.IsMySQL()||b.IsMySQL(){return a.IsMySQL()&&b.IsMySQL()&&a.AccountID==b.AccountID}
    return Target(a)==Target(b)
}

func newAccountID()(string,error){var id [12]byte;_,err:=rand.Read(id[:]);return fmt.Sprintf("janusacc-%x-%x-%x-%x",id[:2],id[2:4],id[4:6],id[6:]),err}
func initialCredentialID(id string)string{sum:=sha256.Sum256([]byte(id));return fmt.Sprintf("account:%x",sum[:16])}

// Account records are public metadata. Keychain items use the opaque stable ID;
// no password, token or DSN is serialized into the registry.
type Account struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Provider string `json:"provider"`
    Host string `json:"host"`
    Port int `json:"port"`
    User string `json:"user"`
    Revision int `json:"revision"`
    Environment string `json:"environment,omitempty"`
    CredentialID string `json:"credentialId,omitempty"`
}
func validateAccounts(doc document)error{
    if len(doc.Accounts)>2000{return errors.New("account limit reached")}
    ids:=map[string]Account{};names:=map[string]bool{};all:=map[string]bool{}
    for _,p:=range doc.Profiles{k:=identityKey(p.ID);if all[k]{return errors.New("duplicate device/account ID")};all[k]=true}
    for _,a:=range doc.Accounts{
        if (a.CredentialID!=""&&!accountIDPattern.MatchString(a.CredentialID))||!accountIDPattern.MatchString(a.ID)||(a.Provider!="mysql"&&a.Provider!="clobotics")||!hostPattern.MatchString(a.Host)||!userPattern.MatchString(a.User)||a.Port<1||a.Port>65535||a.Revision<1||strings.TrimSpace(a.Name)==""||len(a.Name)>800||strings.ContainsAny(a.Name,"\x00\r\n\t\x1b"){return errors.New("invalid account metadata")}
        if a.Provider=="clobotics"{if a.Host!="iam.clobotics.cn"||a.Port!=443||!ValidCLIEnvironment(a.Environment){return errors.New("invalid CLI provider binding")}}else if a.Environment!=""{return errors.New("environment on non-CLI provider account")}
        k:=identityKey(a.ID);if all[k]{return errors.New("duplicate device/account ID")};all[k]=true
        if _,ok:=ids[a.ID];ok||names[strings.ToLower(a.Name)]{return errors.New("duplicate account identity")};ids[a.ID]=a;names[strings.ToLower(a.Name)]=true
    }
    for _,p:=range doc.Profiles{if p.IsMySQL(){a,ok:=ids[p.AccountID];if !ok||a.Provider!="mysql"||a.Host!=p.Host||a.Port!=p.Port||a.User!=p.User{return errors.New("database account binding mismatch")}}}
    return nil
}
func(s Store)Accounts()([]Account,error){doc,err:=s.readDocument();if doc.Accounts==nil{doc.Accounts=[]Account{}};return doc.Accounts,err}

// PrepareMySQL is inert: authentication must succeed before RegisterMySQL.
func(s Store)PrepareMySQL(user,host string,port int,database,ca string,tlsMode ...string)(janus.Profile,Account,bool,error){
    doc,err:=s.readDocument();if err!=nil{return janus.Profile{},Account{},false,err}
    host=strings.ToLower(host);a:=Account{Provider:"mysql",Host:host,Port:port,User:user,Name:user+"@"+host,Revision:1}
    for _,saved:=range doc.Accounts{if saved.Provider==a.Provider&&saved.Host==host&&saved.Port==port&&saved.User==user{a=saved;break}}
    if a.ID==""{a.ID,err=newAccountID();if err!=nil{return janus.Profile{},a,false,err};a.CredentialID=initialCredentialID(a.ID)}
    p:=janus.Profile{Host:host,User:user,Port:port,Database:database,DatabaseCA:ca,AccountID:a.ID,Platform:"database",Type:"mysql",Name:"mysql:"+user+"@"+host,PersistSeconds:900}
    if len(tlsMode)>0&&tlsMode[0]!="verify"{p.DatabaseTLS=tlsMode[0]}
    if err:=Validate(p);err!=nil{return p,a,false,err}
    for _,saved:=range doc.Profiles{if deviceKey(saved)==deviceKey(p){if saved.DatabaseCA!=ca||saved.DatabaseTLS!=p.DatabaseTLS{return p,a,false,errors.New("saved TLS configuration differs; use the saved device")};return saved,a,false,nil}}
    var id [16]byte;if _,err:=rand.Read(id[:]);err!=nil{return p,a,false,err};id[6]=(id[6]&15)|64;id[8]=(id[8]&63)|128
    p.ID=fmt.Sprintf("database:%x-%x-%x-%x-%x",id[:4],id[4:6],id[6:8],id[8:10],id[10:]);return p,a,true,nil
}
func(s Store)RegisterMySQL(p janus.Profile,a Account)(janus.Profile,error){
    if !p.IsMySQL(){return p,errors.New("not a database device")};if err:=Validate(p);err!=nil{return p,err}
    unlock,err:=s.Lock("registry");if err!=nil{return p,err};defer unlock()
    doc,err:=s.readDocument();if err!=nil{return p,err}
    names:=map[string]bool{}
    for _,row:=range doc.Profiles{if row.ID==p.ID||deviceKey(row)==deviceKey(p){return p,errors.New("device already registered; select it again")};names[strings.ToLower(row.Name)]=true}
    if len(doc.Profiles)>=2000{return p,errors.New("device limit reached")}
    found:=false;accountNames:=map[string]bool{}
    for _,row:=range doc.Accounts{accountNames[strings.ToLower(row.Name)]=true;if row.ID==a.ID{if row!=a{return p,errors.New("account changed during authentication")};found=true}else if row.Provider==a.Provider&&row.Host==a.Host&&row.Port==a.Port&&row.User==a.User{return p,errors.New("account registered concurrently; retry")}}
    if !found{base:=a.Name;for n:=2;accountNames[strings.ToLower(a.Name)];n++{a.Name=fmt.Sprintf("%s (%d)",base,n)};doc.Accounts=append(doc.Accounts,a)}
    base:=p.Name;for n:=2;names[strings.ToLower(p.Name)];n++{p.Name=fmt.Sprintf("%s (%d)",base,n)}
    doc.Profiles=append(doc.Profiles,p);return p,s.saveDocument(doc)
}

func(s Store)ReplaceAccount(before,after Account)error{
    unlock,err:=s.Lock("registry");if err!=nil{return err};defer unlock();doc,err:=s.readDocument();if err!=nil{return err}
    for i,a:=range doc.Accounts{if a.ID==before.ID{if a!=before||after.ID!=before.ID||after.Host!=before.Host||after.Port!=before.Port||after.User!=before.User||after.Provider!=before.Provider||after.Environment!=before.Environment||after.Revision!=before.Revision+1{return errors.New("account changed during verification")};doc.Accounts[i]=after;return s.saveDocument(doc)}}
    return errors.New("account was removed")
}
func(s Store)RemoveAccount(id string,remove func(Account)error)error{
    unlock,err:=s.Lock("registry");if err!=nil{return err};defer unlock();doc,err:=s.readDocument();if err!=nil{return err}
    for _,p:=range doc.Profiles{if p.AccountID==id{return fmt.Errorf("account is referenced by device %s (%s); remove that device first",p.Name,p.ID)}}
    for i,a:=range doc.Accounts{if a.ID==id{if err:=remove(a);err!=nil{return err};doc.Accounts=append(doc.Accounts[:i],doc.Accounts[i+1:]...);return s.saveDocument(doc)}};return errors.New("unknown account")
}
func(s Store)Account(id string)(Account,error){rows,err:=s.Accounts();if err!=nil{return Account{},err};for _,a:=range rows{if a.ID==CanonicalAccountID(id)||strings.EqualFold(a.Name,id){return a,nil}};return Account{},errors.New("unknown account")}

func ValidCLIEnvironment(value string)bool{return value=="dev"||value=="test"||value=="prod"||value=="prod-us"}
func(s Store)PrepareCLIAccount(user,environment string)(Account,error){
    id,err:=newAccountID();if err!=nil{return Account{},err}
    a:=Account{ID:id,CredentialID:initialCredentialID(id),Name:"clobotics:"+environment+":"+user,Provider:"clobotics",Host:"iam.clobotics.cn",Port:443,User:user,Environment:environment,Revision:1}
    if err:=validateAccounts(document{Accounts:[]Account{a}});err!=nil{return Account{},err}
    rows,err:=s.Accounts();if err!=nil{return Account{},err};for _,row:=range rows{if row.Provider==a.Provider&&row.Environment==environment&&row.User==user{return Account{},errors.New("CLI provider account already exists")}};return a,nil
}
func(s Store)RegisterCLIAccount(a Account)error{
    if a.Provider!="clobotics"{return errors.New("invalid CLI provider provider")}
    unlock,err:=s.Lock("registry");if err!=nil{return err};defer unlock();doc,err:=s.readDocument();if err!=nil{return err}
    for _,row:=range doc.Accounts{if row.Provider==a.Provider&&row.Environment==a.Environment&&row.User==a.User{return errors.New("CLI provider account registered concurrently")}}
    doc.Accounts=append(doc.Accounts,a);return s.saveDocument(doc)
}
