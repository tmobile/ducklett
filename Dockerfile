# We need a shell for using update-azure-image-tags binary, so alpine it is
FROM alpine:3.13.5
LABEL maintainer="Justin Hopper <justin.hopper6@t-mobile.com>"
COPY ducklett /ducklett
COPY update-azure-image-tags /update-azure-image-tags
CMD ["/ducklett"]
