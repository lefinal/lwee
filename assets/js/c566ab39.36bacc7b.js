"use strict";(self.webpackChunkdocs=self.webpackChunkdocs||[]).push([[1128],{7245:(e,t,n)=>{n.r(t),n.d(t,{assets:()=>c,contentTitle:()=>s,default:()=>d,frontMatter:()=>a,metadata:()=>r,toc:()=>l});var i=n(5893),o=n(1151);const a={},s="Workspace File",r={id:"action-inputs/workspace-file",title:"Workspace File",description:"workspace-file",source:"@site/docs/action-inputs/workspace-file.mdx",sourceDirName:"action-inputs",slug:"/action-inputs/workspace-file",permalink:"/lwee/action-inputs/workspace-file",draft:!1,unlisted:!1,editUrl:"https://github.com/lefinal/lwee/docs/docs/action-inputs/workspace-file.mdx",tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"Stream",permalink:"/lwee/action-inputs/stream"},next:{title:"Action Outputs",permalink:"/lwee/category/action-outputs"}},c={},l=[{value:"Configuration",id:"configuration",level:2}];function p(e){const t={a:"a",admonition:"admonition",code:"code",em:"em",h1:"h1",h2:"h2",p:"p",pre:"pre",...(0,o.a)(),...e.components};return(0,i.jsxs)(i.Fragment,{children:[(0,i.jsx)(t.h1,{id:"workspace-file",children:"Workspace File"}),"\n",(0,i.jsx)(t.p,{children:(0,i.jsx)(t.code,{children:"workspace-file"})}),"\n",(0,i.jsxs)(t.p,{children:["A workspace file provides a file in the action's working directory.\nFor containerized actions like ",(0,i.jsx)(t.a,{href:"/lwee/actions/project-action",children:"project actions"}),", the workspace will be mounted into the container \u2013 usually at ",(0,i.jsx)(t.em,{children:"/lwee"}),"."]}),"\n",(0,i.jsx)(t.admonition,{type:"info",children:(0,i.jsx)(t.p,{children:"Keep in mind that in order for an action to be started, all inputs need to be available.\nThis also means that files need to be fully written.\nIf your actions exchange data through files, this means that they cannot be run in parallel as the previous action needs to finish before the next action can start."})}),"\n",(0,i.jsx)(t.h2,{id:"configuration",children:"Configuration"}),"\n",(0,i.jsx)(t.pre,{children:(0,i.jsx)(t.code,{className:"language-yaml",children:'source: ""\nprovideAs: workspace-file\n# The filename, relative to the workspace directory.\nfilename: "my-file.csv"\n'})}),"\n",(0,i.jsxs)(t.p,{children:["For containerized actions like ",(0,i.jsx)(t.a,{href:"/lwee/actions/project-action",children:"project actions"}),", the filename must not be absolute as only the workspace directory will be mounted to the container."]}),"\n",(0,i.jsx)(t.admonition,{type:"tip",children:(0,i.jsx)(t.p,{children:"As with containerized actions, stick to using relative filenames so that files are placed in the action's working directory.\nThis ensures better compatibility among machines.\nIf you need to provide an absolute filepath in actions, you can use templating to get the absolute path."})})]})}function d(e={}){const{wrapper:t}={...(0,o.a)(),...e.components};return t?(0,i.jsx)(t,{...e,children:(0,i.jsx)(p,{...e})}):p(e)}},1151:(e,t,n)=>{n.d(t,{Z:()=>r,a:()=>s});var i=n(7294);const o={},a=i.createContext(o);function s(e){const t=i.useContext(a);return i.useMemo((function(){return"function"==typeof e?e(t):{...t,...e}}),[t,e])}function r(e){let t;return t=e.disableParentContext?"function"==typeof e.components?e.components(o):e.components||o:s(e.components),i.createElement(a.Provider,{value:t},e.children)}}}]);