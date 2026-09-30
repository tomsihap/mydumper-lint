# Changelog

## 0.1.0 (2026-09-30)


### Features

* add configuration file support and ** globs ([32c221e](https://github.com/tomsihap/mydumper-lint/commit/32c221e79b594bfa19e8fd357a1e02e64c5507f1))
* add effective model, fixer engine and test helpers ([a78464e](https://github.com/tomsihap/mydumper-lint/commit/a78464ea1da1086b2e5d23bf450a104b5f8eb0f6))
* add Go module skeleton and shared contracts ([94aa66d](https://github.com/tomsihap/mydumper-lint/commit/94aa66db12eac66287ed55162dd32d29892f0c38))
* add rule framework, lint pipeline and the loadability rules ([55e688b](https://github.com/tomsihap/mydumper-lint/commit/55e688b2a8eb539f0563d929d60e2b8b5f9fa413))
* apply GOption semantics in the model; add rules MDL302 and MDL401-MDL409 ([5fa9b2e](https://github.com/tomsihap/mydumper-lint/commit/5fa9b2ef565010aaeed6702cacccddcd4910eaed))
* **cli:** add check, inspect, rules, explain, config and completion commands ([9f75e3b](https://github.com/tomsihap/mydumper-lint/commit/9f75e3b1e503ec5bc14705af6a7d298012f79aac))
* **cli:** add the versions command ([7559e4a](https://github.com/tomsihap/mydumper-lint/commit/7559e4ad68b3c79e07b15297c5e0c215483eb5dc))
* emulate mydumper's pre-processor and GLib's GKeyFile parser ([582736a](https://github.com/tomsihap/mydumper-lint/commit/582736a9211572ba6767a954c3252917586fdd49))
* **goption:** emulate GLib's GOption parser for mydumper's option vectors ([52b11bb](https://github.com/tomsihap/mydumper-lint/commit/52b11bb9ecbf4fe4798719b30b7b8f082cd22093))
* group rules MDL201-MDL205; the fixer guards health pass by pass ([60ca3e3](https://github.com/tomsihap/mydumper-lint/commit/60ca3e33326600484243726f8624624a10dd8619))
* init repository ([476dddb](https://github.com/tomsihap/mydumper-lint/commit/476dddba6c7b8e0458af1bcb053be4c48fedddbf))
* lint v0.19.1-x files the way GLib reads them, without the pre-processor ([eb42fe0](https://github.com/tomsihap/mydumper-lint/commit/eb42fe07437e05a93407c9435c64b33150a1ead1))
* load sets, conventions and the connection rules (MDL508, MDL509, MDL601, MDL603, MDL901-MDL905) ([f542fa0](https://github.com/tomsihap/mydumper-lint/commit/f542fa07537cfc8449fe2409147439c6ee3f2719))
* load-set model, inspect --load-set and MDL510 (F15) ([3837fc2](https://github.com/tomsihap/mydumper-lint/commit/3837fc2b157aa15112ea52075bd9725527a2f3cc))
* **lsp:** mydumper-lint server, a language server for editors (M6) ([6676e9f](https://github.com/tomsihap/mydumper-lint/commit/6676e9f55ed295049fd9eeb328c33b01605e8fcd))
* MDL511, mydumper v0.21.2-2 and v0.21.2-3 lose every table section (F17) ([2d741a5](https://github.com/tomsihap/mydumper-lint/commit/2d741a5d38801e808d893f53d52cd8e8779acf56))
* **model:** parse the option groups of rejected files for the rules ([0f250b0](https://github.com/tomsihap/mydumper-lint/commit/0f250b0760218c29ea38518a7bab18ffa51b6f6b))
* **optionsdb:** add C extractor and API skeleton for the knowledge base ([bae2020](https://github.com/tomsihap/mydumper-lint/commit/bae2020f9c0893827c32146ebddf50d3edb50467))
* **optionsdb:** add gen-optionsdb pipeline and first generated data ([3ffb646](https://github.com/tomsihap/mydumper-lint/commit/3ffb6464c7955ac984161c959e4a5046d9670b1f))
* **optionsdb:** add the overlay of regex and closed-set values ([2662e11](https://github.com/tomsihap/mydumper-lint/commit/2662e11b12b5ef9024057ccd20ea6748852f09bc))
* **optionsdb:** record the official image cross-check ([194020c](https://github.com/tomsihap/mydumper-lint/commit/194020c74e76d6d498f1d031d36ce102d6e7d5e0))
* **optionsdb:** record which versions run the config pre-processor ([3f52ba0](https://github.com/tomsihap/mydumper-lint/commit/3f52ba009939ee3a43934643467c2915b737e7a5))
* **oracle:** add --json, --serve and --goption-cases modes ([938fb9f](https://github.com/tomsihap/mydumper-lint/commit/938fb9fe9757067283c1f2d0132c44401af8273e))
* **playground:** mydumper-lint in the browser (M6) ([6edd355](https://github.com/tomsihap/mydumper-lint/commit/6edd3557791bd80560c54583bc2c3ea0d5a04695))
* **report:** add github output format ([f71eddc](https://github.com/tomsihap/mydumper-lint/commit/f71eddcf9e3f1caa92ec5a658dc12285d58b522c))
* **report:** add gitlab output format ([f288354](https://github.com/tomsihap/mydumper-lint/commit/f2883540b550e053949932a98fe3df693ee91fe5))
* **report:** add json output format and its schema ([2e035f2](https://github.com/tomsihap/mydumper-lint/commit/2e035f24018ee5cccd2953348222ff8d0953095e))
* **report:** add junit output format ([38e4d4a](https://github.com/tomsihap/mydumper-lint/commit/38e4d4a928bde1117c6bc3d9d9e2a333279f4141))
* **report:** add sarif output format ([8a578ff](https://github.com/tomsihap/mydumper-lint/commit/8a578ff5dbe6002ad9db7e212ef982d35b9d8a58))
* **report:** add text and concise output formats ([67ded54](https://github.com/tomsihap/mydumper-lint/commit/67ded543dbeb4e40e7c375cbc60a9dbdf4848680))
* **rules:** add MDL602 mysql-include-directive; MDL405 checks every group ([82bf399](https://github.com/tomsihap/mydumper-lint/commit/82bf399e0451fa3fe206f6463e7bf87f8a3b35f7))
* **rules:** suppression comments with MDL001 and MDL002; safer MDL401 renames ([01722c0](https://github.com/tomsihap/mydumper-lint/commit/01722c0fa8673ed52bb41318b61e6108eb75a229))
* **rules:** table and masking rules MDL501-MDL507; MDL108 fixes leak chains in one pass ([6ee02fa](https://github.com/tomsihap/mydumper-lint/commit/6ee02faf54cc96774a5078bc783c315eac3a7bc2))
* **rules:** value rules MDL301, MDL303-MDL305, MDL307, MDL308, MDL311, MDL313 ([fc581e6](https://github.com/tomsihap/mydumper-lint/commit/fc581e635d105b092a1f143e92bcaf80c706b295))
* **vscode:** bundle mydumper-lint in platform-specific packages ([d856a0a](https://github.com/tomsihap/mydumper-lint/commit/d856a0ab3c881ba2804f0f8b505aa12efe05c4c5))


### Bug Fixes

* **fix:** stop the fixpoint when a pass can apply nothing ([63bc18f](https://github.com/tomsihap/mydumper-lint/commit/63bc18fa2b518b2b44cb587e4d12a18750317054))
* **gen-optionsdb:** clear the golangci-lint findings ([e102764](https://github.com/tomsihap/mydumper-lint/commit/e1027647b0eafcfa10f3019bea8ebc0f72be1058))
* make every safe fix converge and preserve the model (found by fuzzing) ([45ec739](https://github.com/tomsihap/mydumper-lint/commit/45ec739a6e9462c76a3e40eab63275362f75cd4d))
* **model:** per-product option groups are read by mydumper from v0.21.2-2 only ([125ad66](https://github.com/tomsihap/mydumper-lint/commit/125ad6632654351727c6bc8c97d27241aa393584))
* **report:** neutral impact label, exhaustive severity switches ([852a1af](https://github.com/tomsihap/mydumper-lint/commit/852a1af767fa4b1648acbfa6f09238cf5ae55326))
* Windows portability of paths in messages and tests ([692d1e1](https://github.com/tomsihap/mydumper-lint/commit/692d1e11d2e86801cc4c86afc300429592e8fe48))


### Performance Improvements

* 5x faster checks, 7x less memory on large runs ([e59b937](https://github.com/tomsihap/mydumper-lint/commit/e59b9371ad80161e5dced4ab6ff1d4435c8c0a7e))
* **report:** avoid an allocation per ASCII character in excerpts ([69e6a67](https://github.com/tomsihap/mydumper-lint/commit/69e6a6706f8d26bc5fd6b463cad87efee23773d3))


### Documentation

* add design document ([1a83337](https://github.com/tomsihap/mydumper-lint/commit/1a833372b064776bf01ab8c2b6b3817303ce1257))
* add license, notice, code of conduct and security policy ([52ff83a](https://github.com/tomsihap/mydumper-lint/commit/52ff83a2fbe4ab1614a14f9dfca9955455b2da83))
* installing the VS Code extension, and releasing it ([3ec3cf1](https://github.com/tomsihap/mydumper-lint/commit/3ec3cf1570c073ae9d63323689150bfba79dbe75))
* **optionsdb:** say that a DB must come from Load or Parse ([362ca3b](https://github.com/tomsihap/mydumper-lint/commit/362ca3bdd5bd433339c9ea906175e67584f51d94))
* **oracle:** add README and the GPL-3.0 license text ([b1926f3](https://github.com/tomsihap/mydumper-lint/commit/b1926f3e58dde7e352c352eb6dc165fcc8b83689))
* README for users and contributors, CONTRIBUTING, generated rule docs ([da37295](https://github.com/tomsihap/mydumper-lint/commit/da3729543a61fd98029e1dbc59ce6979dc80fb1f))
