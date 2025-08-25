FROM debian:12

RUN apt-get -y update
RUN apt-get -y upgrade
RUN apt-get -y install wget
RUN apt-get -y install git
RUN apt-get -y install build-essential
RUN apt-get -y install autoconf automake gdb git libffi-dev zlib1g-dev libssl-dev
RUN apt-get -y install python3
RUN apt-get -y upgrade


# Install golang 1.24
RUN mkdir -p /usr/local
WORKDIR /usr/local
RUN wget https://go.dev/dl/go1.24.6.linux-amd64.tar.gz
RUN tar -C /usr/local -xzf go1.24.6.linux-amd64.tar.gz
ENV PATH=$PATH:/usr/local/go/bin

# make directories
RUN mkdir /var/lib/asterisk

WORKDIR /usr/src
RUN mkdir asterisk
RUN mkdir asteriskvoicebot

WORKDIR /etc
RUN mkdir asterisk

# clone Asterisk
WORKDIR /usr/src/asterisk
RUN git clone "https://github.com/asterisk/asterisk.git"

# go to our new clone
WORKDIR /usr/src/asterisk/asterisk

# checkout version 22
RUN git checkout 22

# Install it.
RUN contrib/scripts/install_prereq install
RUN ./configure --enable-dev-mode --disable-binary-modules --with-jansson-bundled --with-pjproject-bundled
RUN make menuselect.makeopts
RUN make
RUN make install

# Add our configuration from local
ADD ast-config /etc/asterisk

# Add our text to speech cache
ADD tts_cache /var/lib/asterisk/sounds/en

# Add our chatbot from local
ADD asteriskvoicebot.tar.gz /usr/src/asteriskvoicebot
WORKDIR /usr/src/asteriskvoicebot
RUN go install .

# Ready to rock and roll
WORKDIR /usr/src/asterisk