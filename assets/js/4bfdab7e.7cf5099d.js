"use strict";(self.webpackChunkdocs=self.webpackChunkdocs||[]).push([[2272],{2912:(e,n,i)=>{i.r(n),i.d(n,{assets:()=>a,contentTitle:()=>l,default:()=>h,frontMatter:()=>t,metadata:()=>o,toc:()=>c});var r=i(5893),s=i(1151);const t={sidebar_position:20},l="Usage",o={id:"usage",title:"Usage",description:"Make sure to have LWEE properly set up by running:",source:"@site/docs/usage.mdx",sourceDirName:".",slug:"/usage",permalink:"/lwee/usage",draft:!1,unlisted:!1,editUrl:"https://github.com/lefinal/lwee/docs/docs/usage.mdx",tags:[],version:"current",sidebarPosition:20,frontMatter:{sidebar_position:20},sidebar:"tutorialSidebar",previous:{title:"Concepts",permalink:"/lwee/concepts"},next:{title:"Actions",permalink:"/lwee/actions/"}},a={},c=[{value:"Commands",id:"commands",level:2},{value:"run, r",id:"run-r",level:3},{value:"verify, v",id:"verify-v",level:3},{value:"create-action, ca",id:"create-action-ca",level:3},{value:"init",id:"init",level:3},{value:"version",id:"version",level:3},{value:"help",id:"help",level:3},{value:"Flags",id:"flags",level:2},{value:"--verbose, -v",id:"--verbose--v",level:3},{value:"--dir",id:"--dir",level:3},{value:"--flow, -f",id:"--flow--f",level:3},{value:"--no-cleanup",id:"--no-cleanup",level:3},{value:"--engine",id:"--engine",level:3},{value:"--help, -h",id:"--help--h",level:3}];function d(e){const n={a:"a",code:"code",em:"em",h1:"h1",h2:"h2",h3:"h3",li:"li",p:"p",pre:"pre",strong:"strong",ul:"ul",...(0,s.a)(),...e.components};return(0,r.jsxs)(r.Fragment,{children:[(0,r.jsx)(n.h1,{id:"usage",children:"Usage"}),"\n",(0,r.jsx)(n.p,{children:"Make sure to have LWEE properly set up by running:"}),"\n",(0,r.jsx)(n.pre,{children:(0,r.jsx)(n.code,{className:"language-shell",children:"lwee version\n"})}),"\n",(0,r.jsx)(n.p,{children:"The command syntax is:"}),"\n",(0,r.jsx)(n.pre,{children:(0,r.jsx)(n.code,{children:"lwee [options] command\n"})}),"\n",(0,r.jsxs)(n.p,{children:["When running commands, LWEE searches in the current or parent directory for an LWEE project.\nThese are identified by having a ",(0,r.jsx)(n.em,{children:".lwee"})," directory in the project root.\nAlternatively, you can specify the project path manually using ",(0,r.jsx)(n.a,{href:"#--dir",children:"--dir"}),"."]}),"\n",(0,r.jsx)(n.h2,{id:"commands",children:"Commands"}),"\n",(0,r.jsxs)(n.p,{children:["LWEE provides multiple commands for workflow programming and execution.\nIf you run the application in an LWEE project, you will be prompted with a command selection.\nSelect a command from the list and then press ",(0,r.jsx)("kbd",{children:"ENTER"})," to execute."]}),"\n",(0,r.jsx)(n.p,{children:"Alternatively, you can specify the command directly when invoking LWEE."}),"\n",(0,r.jsx)(n.h3,{id:"run-r",children:"run, r"}),"\n",(0,r.jsx)(n.p,{children:"Run the specified workflow in the current project."}),"\n",(0,r.jsx)(n.h3,{id:"verify-v",children:"verify, v"}),"\n",(0,r.jsx)(n.p,{children:"Perform a dry run of the workflow but do not execute actions."}),"\n",(0,r.jsx)(n.h3,{id:"create-action-ca",children:"create-action, ca"}),"\n",(0,r.jsx)(n.p,{children:"Create a project action in the current project.\nYou will be prompted with the name of the action to create.\nIt is recommended to name your action in kebab-case.\nYou will then be asked to select an action template.\nThe following options are available:"}),"\n",(0,r.jsxs)(n.ul,{children:["\n",(0,r.jsxs)(n.li,{children:[(0,r.jsx)(n.strong,{children:"None"}),": This will initialize the action with no files.\nIt is your responsibility to set up a proper ",(0,r.jsx)(n.a,{href:"https://docs.docker.com/engine/reference/builder/",children:"Dockerfile"}),"."]}),"\n",(0,r.jsxs)(n.li,{children:[(0,r.jsx)(n.strong,{children:"Go (SDK)"}),": Creates a Go application with an LWEE Go SDK template and ready-to-use ",(0,r.jsx)(n.a,{href:"https://docs.docker.com/engine/reference/builder/",children:"Dockerfile"}),".\nCreating the action might take a while as LWEE will set up the latest version of the SDK."]}),"\n",(0,r.jsxs)(n.li,{children:[(0,r.jsx)(n.strong,{children:"Go (native)"}),": Creates a Go application without any dependencies as well as a ready-to-use ",(0,r.jsx)(n.a,{href:"https://docs.docker.com/engine/reference/builder/",children:"Dockerfile"}),"."]}),"\n"]}),"\n",(0,r.jsxs)(n.p,{children:["You can find the created project action in the ",(0,r.jsx)(n.em,{children:"/actions"})," directory."]}),"\n",(0,r.jsx)(n.h3,{id:"init",children:"init"}),"\n",(0,r.jsx)(n.p,{children:"Initializes a new LWEE project in the current working directory.\nThis includes setting up directories as well as creating a flow file template."}),"\n",(0,r.jsx)(n.h3,{id:"version",children:"version"}),"\n",(0,r.jsx)(n.p,{children:"Prints the installed version of LWEE."}),"\n",(0,r.jsx)(n.h3,{id:"help",children:"help"}),"\n",(0,r.jsx)(n.p,{children:"Prints a help summary."}),"\n",(0,r.jsx)(n.h2,{id:"flags",children:"Flags"}),"\n",(0,r.jsx)(n.h3,{id:"--verbose--v",children:"--verbose, -v"}),"\n",(0,r.jsx)(n.p,{children:"Enabled output of debug logs."}),"\n",(0,r.jsx)(n.h3,{id:"--dir",children:"--dir"}),"\n",(0,r.jsx)(n.p,{children:"Specifies the location of the project directory.\nIf not set, the current working directory and its parents will be searched for an LWEE project and used instead."}),"\n",(0,r.jsx)(n.h3,{id:"--flow--f",children:"--flow, -f"}),"\n",(0,r.jsxs)(n.p,{children:["Specifies the flow file to use.\nIf a non-absolute file path is provided, the LWEE project's path will be used as the base.\nDefaults to ",(0,r.jsx)(n.em,{children:"flow.yaml"}),"."]}),"\n",(0,r.jsx)(n.h3,{id:"--no-cleanup",children:"--no-cleanup"}),"\n",(0,r.jsx)(n.p,{children:"If set, temporary files and containers will not be cleaned up.\nThis is useful for debugging purposes.\nMake sure to clean up possibly dangling containers yourself."}),"\n",(0,r.jsxs)(n.p,{children:["This flag can also be set using environment variables.\nSet ",(0,r.jsx)(n.code,{children:"LWEE_NO_CLEANUP"})," to ",(0,r.jsx)(n.code,{children:"true"})," to disable cleanup."]}),"\n",(0,r.jsx)(n.h3,{id:"--engine",children:"--engine"}),"\n",(0,r.jsx)(n.p,{children:"Specifies the container engine to use.\nPossible values are:"}),"\n",(0,r.jsxs)(n.ul,{children:["\n",(0,r.jsx)(n.li,{children:(0,r.jsx)(n.em,{children:"docker"})}),"\n"]}),"\n",(0,r.jsxs)(n.p,{children:["If not set, ",(0,r.jsx)(n.em,{children:"docker"})," will be used."]}),"\n",(0,r.jsxs)(n.p,{children:["This flag can also be set using environment variables.\nSet ",(0,r.jsx)(n.code,{children:"LWEE_ENGINE"})," to the desired value."]}),"\n",(0,r.jsx)(n.h3,{id:"--help--h",children:"--help, -h"}),"\n",(0,r.jsx)(n.p,{children:"Prints a help summary."})]})}function h(e={}){const{wrapper:n}={...(0,s.a)(),...e.components};return n?(0,r.jsx)(n,{...e,children:(0,r.jsx)(d,{...e})}):d(e)}},1151:(e,n,i)=>{i.d(n,{Z:()=>o,a:()=>l});var r=i(7294);const s={},t=r.createContext(s);function l(e){const n=r.useContext(t);return r.useMemo((function(){return"function"==typeof e?e(n):{...n,...e}}),[n,e])}function o(e){let n;return n=e.disableParentContext?"function"==typeof e.components?e.components(s):e.components||s:l(e.components),r.createElement(t.Provider,{value:n},e.children)}}}]);