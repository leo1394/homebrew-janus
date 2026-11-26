package store

import (
    "os"
    "path/filepath"
    "testing"
    "strings"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)

func TestMySQLRegistrationAndAccountIsolation(t *testing.T){
    s:=Store{Root:filepath.Join(t.TempDir(),"base")}
    ssh,_,err:=s.Add("reader","example.com",3306);if err!=nil{t.Fatal(err)}
    before,err:=os.ReadFile(filepath.Join(s.Root,"devices.json"));if err!=nil{t.Fatal(err)}
    p,a,pending,err:=s.PrepareMySQL("reader","example.com",3306,"analytics","");if err!=nil||!pending{t.Fatal(err)}
    after,_:=os.ReadFile(filepath.Join(s.Root,"devices.json"));if string(before)!=string(after){t.Fatal("prepare wrote account/device before authentication")}
    p,err=s.RegisterMySQL(p,a);if err!=nil{t.Fatal(err)}
    if sameCredential(ssh,p){t.Fatal("database shares SSH credential")}
    q,b,pending,err:=s.PrepareMySQL("reader","example.com",3306,"other","");if err!=nil||!pending||b.ID!=a.ID{t.Fatal("same account not reused",err)}
    q,err=s.RegisterMySQL(q,b);if err!=nil{t.Fatal(err)}
    if !sameCredential(p,q){t.Fatal("database account relation missing")}
    _,_,pending,err=s.PrepareMySQL("reader","example.com",3306,"analytics","");if err!=nil||pending{t.Fatal("existing device not found",err)}
    called:=false;if err:=s.Remove(p.ID,func(_ janus.Profile)error{called=true;return nil});err!=nil||called{t.Fatal("shared database credential removed",err)}
    accounts,err:=s.Accounts();if err!=nil||len(accounts)!=1{t.Fatal("account metadata lost",err)}
}

func TestAccountReplacementCompareAndSwap(t *testing.T){
    s:=Store{Root:filepath.Join(t.TempDir(),"base")}
    p,a,_,err:=s.PrepareMySQL("reader","example.com",3306,"","");if err!=nil{t.Fatal(err)}
    p,err=s.RegisterMySQL(p,a);if err!=nil{t.Fatal(err)}
    next:=a;next.Revision++;next.CredentialID="account:00000000000000000000000000000001"
    if err:=s.ReplaceAccount(a,next);err!=nil{t.Fatal(err)}
    if err:=s.ReplaceAccount(a,next);err==nil{t.Fatal("stale verification overwrote account")}
    called:=false;if err:=s.RemoveAccount(a.ID,func(Account)error{called=true;return nil});err==nil||called{t.Fatal("referenced account deleted")}
    got,err:=s.Get(p.ID);if err!=nil||got.AccountID!=a.ID{t.Fatal("replacement changed target binding",err)}
}

func TestAccountIDMigrationPreservesKeychainAndBindings(t *testing.T){
    s:=Store{Root:filepath.Join(t.TempDir(),"base")}
    p,a,_,err:=s.PrepareMySQL("reader","example.com",3306,"","");if err!=nil{t.Fatal(err)}
    if len(a.ID)!=36||!strings.HasPrefix(a.ID,"janusacc-"){t.Fatal("invalid account namespace")}
    old:="account:0123456789abcdef0123456789abcdef";a.ID=old;a.CredentialID="";p.AccountID=old
    doc:=document{Schema:1,Accounts:[]Account{a},Profiles:[]janus.Profile{p}}
    normalizeAccounts(&doc)
    if doc.Accounts[0].CredentialID!=old||doc.Accounts[0].ID!=CanonicalAccountID(old)||doc.Profiles[0].AccountID!=doc.Accounts[0].ID{t.Fatal("migration changed credential or lost relation")}
    if err:=s.saveDocument(doc);err!=nil{t.Fatal(err)}
    if CanonicalAccountID("janusacc-acc063ec-6fd3-ca2d-d086-0413b51103a6")!="janusacc-6fd3-ca2d-d086-0413b51103a6"{t.Fatal("previous ID alias lost")}
    got,err:=s.Account(old);if err!=nil||got.ID!=doc.Accounts[0].ID||got.CredentialID!=old{t.Fatal("legacy lookup failed",err)}
    p.ID="database:"+strings.TrimPrefix(got.ID,"janusacc-");p.AccountID=got.ID
    if err:=s.saveDocument(document{Schema:1,Accounts:[]Account{got},Profiles:[]janus.Profile{p}});err==nil{t.Fatal("cross-kind ID collision accepted")}
}
